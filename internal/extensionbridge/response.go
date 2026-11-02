package extensionbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Decision is a public wire DTO, independent from the extension Go module.
type Decision struct {
	RequestID           string   `json:"request_id"`
	TenantID            uint64   `json:"tenant_id"`
	KnowledgeBaseIDs    []string `json:"knowledge_base_ids"`
	KnowledgeIDs        []string `json:"knowledge_ids"`
	ExcludedDocumentIDs []string `json:"excluded_document_ids"`
	DenyAll             bool     `json:"deny_all"`
}

var errInvalidDecision = errors.New("invalid before_retrieve policy response")

func decodeDecision(data, request []byte) (d Decision, err error) {
	if len(data) > 1<<20 {
		return d, errInvalidDecision
	}
	// All six canonical fields are required, unique and non-null. Values are
	// scalars/string lists; typed decoding below rejects nested/incorrect types.
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return d, errInvalidDecision
	}
	fields := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return d, errInvalidDecision
		}
		name, ok := key.(string)
		if !ok || fields[name] {
			return d, errInvalidDecision
		}
		switch name {
		case "request_id", "tenant_id", "knowledge_base_ids", "knowledge_ids", "excluded_document_ids", "deny_all":
		default:
			return d, errInvalidDecision
		}
		fields[name] = true
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return d, errInvalidDecision
		}
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') || len(fields) != 6 {
		return d, errInvalidDecision
	}
	if _, err = dec.Token(); err != io.EOF {
		return d, errInvalidDecision
	}
	if json.Unmarshal(data, &d) != nil {
		return d, errInvalidDecision
	}
	var sent struct {
		RequestID        string   `json:"request_id"`
		TenantID         uint64   `json:"tenant_id"`
		KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
		KnowledgeIDs     []string `json:"knowledge_ids"`
	}
	if json.Unmarshal(request, &sent) != nil || sent.RequestID == "" || sent.TenantID == 0 || len(sent.KnowledgeBaseIDs) == 0 || len(sent.KnowledgeBaseIDs) > 1000 || len(sent.KnowledgeIDs) > 10000 || sent.KnowledgeIDs == nil || d.RequestID != sent.RequestID || d.TenantID != sent.TenantID {
		return Decision{}, errInvalidDecision
	}
	kbSet, ok := uniqueSet(sent.KnowledgeBaseIDs)
	if !ok || len(d.KnowledgeBaseIDs) != len(kbSet) {
		return Decision{}, errInvalidDecision
	}
	responseKB, ok := uniqueSet(d.KnowledgeBaseIDs)
	if !ok {
		return Decision{}, errInvalidDecision
	}
	for kb := range responseKB {
		if !kbSet[kb] {
			return Decision{}, errInvalidDecision
		}
	}
	authorized, ok := uniqueSet(sent.KnowledgeIDs)
	if !ok {
		return Decision{}, errInvalidDecision
	}
	allow, ok := uniqueSet(d.KnowledgeIDs)
	if !ok {
		return Decision{}, errInvalidDecision
	}
	excluded, ok := uniqueSet(d.ExcludedDocumentIDs)
	if !ok {
		return Decision{}, errInvalidDecision
	}
	if len(allow)+len(excluded) != len(authorized) || d.DenyAll != (len(allow) == 0) {
		return Decision{}, errInvalidDecision
	}
	for id := range allow {
		if !authorized[id] || excluded[id] {
			return Decision{}, errInvalidDecision
		}
	}
	for id := range excluded {
		if !authorized[id] {
			return Decision{}, errInvalidDecision
		}
	}
	return d, nil
}
func uniqueSet(ids []string) (map[string]bool, bool) {
	if ids == nil {
		return nil, false
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > 256 || set[id] {
			return nil, false
		}
		set[id] = true
	}
	return set, true
}
