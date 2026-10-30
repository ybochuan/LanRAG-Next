// Package contract defines public Bridge data without upstream model imports.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxRequestBytes bounds the entire serialized before_retrieve request.
const MaxRequestBytes = 1 << 20

// ErrInvalidRequest deliberately excludes caller data and decoder details.
var ErrInvalidRequest = errors.New("invalid before_retrieve request")

// Context carries the frozen pre-retrieval fields. KnowledgeIDs must be the
// already enumerated native-authorized document set, never a whole-KB sentinel.
// TagScopes retains native tag IDs by KB; callers must preserve these filters.
// Metadata values stay raw JSON so integers and extension values survive IPC.
type Context struct {
	RequestID        string                     `json:"request_id"`
	SessionID        string                     `json:"session_id"`
	Mode             string                     `json:"mode"`
	AgentRound       int                        `json:"agent_round"`
	TenantID         uint64                     `json:"tenant_id"`
	KnowledgeBaseIDs []string                   `json:"knowledge_base_ids"`
	KnowledgeIDs     []string                   `json:"knowledge_ids"`
	TagScopes        map[string][]string        `json:"tag_scopes"`
	OriginalQuery    string                     `json:"original_query"`
	RewrittenQuery   string                     `json:"rewritten_query"`
	TokenBudget      int                        `json:"token_budget"`
	Metadata         map[string]json.RawMessage `json:"metadata"`
}

// BeforeRetrieveRequest supplies context after native authorization/enumeration.
type BeforeRetrieveRequest struct{ Context }

// BeforeRetrieveResponse only narrows input IDs. An empty KnowledgeIDs is always
// accompanied by DenyAll, which a native adapter must treat as a hard empty scope.
// ExcludedDocumentIDs is descriptive; sending it alone to native search is unsafe.
type BeforeRetrieveResponse struct {
	RequestID           string   `json:"request_id"`
	TenantID            uint64   `json:"tenant_id"`
	KnowledgeBaseIDs    []string `json:"knowledge_base_ids"`
	KnowledgeIDs        []string `json:"knowledge_ids"`
	ExcludedDocumentIDs []string `json:"excluded_document_ids"`
	DenyAll             bool     `json:"deny_all"`
}

func validID(id string) bool {
	return len(id) > 0 && len(id) <= 256 && id != "." && id != ".." &&
		!strings.ContainsAny(id, "/\\?#%") && strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0 && utf8.ValidString(id)
}

func uniqueIDs(ids []string, max int) bool {
	if ids == nil || len(ids) > max {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// ValidateScope validates a bounded, nonempty tenant/KB configuration or request.
func (c Context) ValidateScope() error {
	if c.TenantID == 0 || len(c.KnowledgeBaseIDs) == 0 || !uniqueIDs(c.KnowledgeBaseIDs, 1000) {
		return ErrInvalidRequest
	}
	return nil
}

// Validate refuses unknown document scope; an explicit [] is a valid empty scope.
func (r BeforeRetrieveRequest) Validate() error {
	validTrace := func(s string) bool {
		return len(s) > 0 && len(s) <= 256 && strings.TrimSpace(s) != "" && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
	}
	if r.ValidateScope() != nil || !validTrace(r.RequestID) || !validTrace(r.SessionID) || (r.Mode != "rag" && r.Mode != "agent") || r.AgentRound < 0 || r.TokenBudget < 0 || len(r.OriginalQuery) > 65536 || len(r.RewrittenQuery) > 65536 || !uniqueIDs(r.KnowledgeIDs, 10000) {
		return ErrInvalidRequest
	}
	allowed := make(map[string]bool, len(r.KnowledgeBaseIDs))
	for _, kb := range r.KnowledgeBaseIDs {
		allowed[kb] = true
	}
	tags := 0
	for kb, ids := range r.TagScopes {
		tags += len(ids)
		if !allowed[kb] || !uniqueIDs(ids, 10000) || tags > 10000 {
			return ErrInvalidRequest
		}
	}
	return nil
}

// DecodeBeforeRetrieve rejects noncanonical field names, duplicate keys at any
// depth, multiple values and null scalar/list fields. Metadata may contain null.
// Malformed input always returns the same non-sensitive error.
func DecodeBeforeRetrieve(data []byte) (BeforeRetrieveRequest, error) {
	var r BeforeRetrieveRequest
	if len(data) > MaxRequestBytes || !utf8.Valid(data) {
		return r, ErrInvalidRequest
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if checkValue(d, 0) != nil {
		return r, ErrInvalidRequest
	}
	if _, err := d.Token(); err != io.EOF {
		return r, ErrInvalidRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return r, ErrInvalidRequest
	}
	for key, value := range fields {
		switch key {
		case "request_id", "session_id", "mode", "agent_round", "tenant_id", "knowledge_base_ids", "knowledge_ids", "tag_scopes", "original_query", "rewritten_query", "token_budget", "metadata":
		default:
			return r, ErrInvalidRequest
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return r, ErrInvalidRequest
		}
	}
	if json.Unmarshal(data, &r) != nil || r.Validate() != nil {
		return BeforeRetrieveRequest{}, ErrInvalidRequest
	}
	return r, nil
}

func checkValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalidRequest
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrInvalidRequest
			}
			seen[name] = true
			if err := checkValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := checkValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalidRequest
	}
	_, err = d.Token() // The decoder enforces the matching closing delimiter.
	return err
}
