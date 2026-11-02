// Package lineage owns extension version facts derived from trusted observed snapshots.
package lineage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Scheme freezes exact native field fingerprints and root encoding.
const Scheme = "agentrag-native-snapshot-v1"

var (
	ErrEvidence    = errors.New("trusted source snapshot unavailable or invalid")
	ErrConflict    = errors.New("observed source head conflicts")
	ErrUnavailable = errors.New("source authority unavailable")
)

var headerNames = []string{"type", "title", "description", "source", "channel", "parse_status", "summary_status", "profile", "enable_status", "file_name", "folder_path", "file_type", "file_size", "file_hash", "metadata", "custom_metadata", "created_at"}

// ChunkLeaf describes one complete native row without disclosing its content.
type ChunkLeaf struct {
	ID              string `json:"id"`
	ContentRevision int    `json:"content_revision"`
	ChunkIndex      int    `json:"chunk_index"`
	ContentHash     string `json:"content_hash"`
	FieldsHash      string `json:"fields_hash"`
}

// Snapshot contains hashes only; the independently encoded root is recomputed.
type Snapshot struct {
	Scheme          string            `json:"scheme"`
	TenantID        uint64            `json:"tenant_id"`
	KnowledgeBaseID string            `json:"knowledge_base_id"`
	DocumentID      string            `json:"document_id"`
	HeaderFields    map[string]string `json:"header_fields"`
	HeaderHash      string            `json:"header_hash"`
	Chunks          []ChunkLeaf       `json:"chunks"`
	RootHash        string            `json:"root_hash"`
}

func sha(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func validID(s string) bool {
	if len(s) > 128 || !utf8.ValidString(s) || !weknora.ValidID(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func headerDigest(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([][2]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, [2]string{key, fields[key]})
	}
	b, _ := json.Marshal(pairs)
	return sha(b)
}
func rootDigest(s Snapshot) string {
	b, _ := json.Marshal(struct {
		Scheme          string      `json:"scheme"`
		TenantID        uint64      `json:"tenant_id"`
		KnowledgeBaseID string      `json:"knowledge_base_id"`
		DocumentID      string      `json:"document_id"`
		HeaderHash      string      `json:"header_hash"`
		Chunks          []ChunkLeaf `json:"chunks"`
	}{s.Scheme, s.TenantID, s.KnowledgeBaseID, s.DocumentID, s.HeaderHash, s.Chunks})
	return sha(b)
}

// Validate refuses incomplete lists, unrecognized schemes and mismatched scope/root.
func (s Snapshot) Validate(scope provenance.Scope, doc string) error {
	if s.Scheme != Scheme || s.TenantID != scope.TenantID || s.KnowledgeBaseID != scope.KnowledgeBaseID || s.DocumentID != doc || scope.TenantID == 0 || !validID(doc) || !validID(scope.KnowledgeBaseID) || len(s.Chunks) == 0 || len(s.Chunks) > 4096 || len(s.HeaderFields) != len(headerNames) {
		return ErrEvidence
	}
	for _, name := range headerNames {
		if !validHash(s.HeaderFields[name]) {
			return ErrEvidence
		}
	}
	if s.HeaderFields["parse_status"] != sha([]byte(`"completed"`)) || s.HeaderHash != headerDigest(s.HeaderFields) || s.RootHash != rootDigest(s) {
		return ErrEvidence
	}
	seen := map[string]bool{}
	for i, c := range s.Chunks {
		if !validID(c.ID) || seen[c.ID] || c.ContentRevision < 0 || c.ChunkIndex < 0 || !validHash(c.ContentHash) || !validHash(c.FieldsHash) {
			return ErrEvidence
		}
		seen[c.ID] = true
		if i > 0 {
			p := s.Chunks[i-1]
			if p.ChunkIndex > c.ChunkIndex || p.ChunkIndex == c.ChunkIndex && p.ID >= c.ID {
				return ErrEvidence
			}
		}
	}
	b, e := json.Marshal(s)
	if e != nil || len(b) > 2<<20 {
		return ErrEvidence
	}
	return nil
}

// SnapshotReader is a trusted server adapter, not a browser-provided snapshot.
type SnapshotReader interface {
	Capture(context.Context, provenance.Scope, string) (Snapshot, error)
}

// HTTPReader calls one fixed native origin with an existing scoped machine credential.
type HTTPReader struct {
	client *weknora.Client
	scopes map[string]bool
}

// NewHTTPReader never accepts a request-supplied URL or new authorization scope.
func NewHTTPReader(client *weknora.Client, kbs []string) (*HTTPReader, error) {
	if client == nil || len(kbs) == 0 {
		return nil, ErrEvidence
	}
	r := &HTTPReader{client: client, scopes: map[string]bool{}}
	for _, kb := range kbs {
		if !validID(kb) || r.scopes[kb] {
			return nil, ErrEvidence
		}
		r.scopes[kb] = true
	}
	return r, nil
}

// Capture rejects errors and never exposes native response bodies or credentials.
func (r *HTTPReader) Capture(ctx context.Context, s provenance.Scope, doc string) (Snapshot, error) {
	if r == nil || r.client == nil || s.TenantID != r.client.TenantID() || !r.scopes[s.KnowledgeBaseID] || !validID(doc) {
		return Snapshot{}, ErrEvidence
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	e := r.client.JSON(ctx, http.MethodGet, "/api/v1/knowledge-bases/"+url.PathEscape(s.KnowledgeBaseID)+"/knowledge/"+url.PathEscape(doc)+"/source-snapshot", nil, nil, &env)
	if e != nil || len(env.Data) > 2<<20 {
		return Snapshot{}, ErrEvidence
	}
	var out Snapshot
	dec := json.NewDecoder(bytes.NewReader(env.Data))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || dec.Decode(new(any)) != io.EOF || out.Validate(s, doc) != nil {
		return Snapshot{}, ErrEvidence
	}
	return out, nil
}
