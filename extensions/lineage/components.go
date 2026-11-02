package lineage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

var chunkFieldNames = []string{"content", "source_content", "content_revision", "index_status", "chunk_index", "is_enabled", "flags", "status", "start_at", "end_at", "pre_chunk_id", "next_chunk_id", "chunk_type", "parent_chunk_id", "relation_chunks", "indirect_relation_chunks", "metadata", "content_hash", "image_info", "context_header", "source_locators", "created_at"}

// Components folds complete native field maps into the unchanged full v1 root.
type Components struct {
	View        string                       `json:"view"`
	Snapshot    Snapshot                     `json:"snapshot"`
	ChunkFields map[string]map[string]string `json:"chunk_fields"`
}

func (v Components) validate(s provenance.Scope, doc, chunkID string) error {
	if v.View != "components-v1" || v.Snapshot.Validate(s, doc) != nil || len(v.ChunkFields) != 1 {
		return ErrEvidence
	}
	fields := v.ChunkFields[chunkID]
	if len(fields) != len(chunkFieldNames) {
		return ErrEvidence
	}
	for _, name := range chunkFieldNames {
		if !validHash(fields[name]) {
			return ErrEvidence
		}
	}
	for _, leaf := range v.Snapshot.Chunks {
		if leaf.ID == chunkID && leaf.FieldsHash == headerDigest(fields) {
			return nil
		}
	}
	return ErrEvidence
}

// ComponentVerifier uses a fixed machine-authorized reader, never caller snapshots.
type ComponentVerifier struct {
	reader   *HTTPReader
	registry *Registry
}

// NewComponentVerifier adapts the native component view to trusted run inputs.
func NewComponentVerifier(reader *HTTPReader, registry *Registry) *ComponentVerifier {
	return &ComponentVerifier{reader, registry}
}

// VerifyInput accepts only exact current raw fields. Unknown transformations are denied.
func (v *ComponentVerifier) VerifyInput(ctx context.Context, s provenance.Scope, e provenance.InputEvidence) ([]provenance.SourceRef, error) {
	if v == nil || v.reader == nil || v.registry == nil || v.reader.client == nil || s.TenantID != v.reader.client.TenantID() || !v.reader.scopes[s.KnowledgeBaseID] || !validID(e.DocumentID) || !validHash(e.ValueHash) {
		return nil, ErrEvidence
	}
	var snapshot Snapshot
	var actual string
	if e.ChunkID == "" {
		var err error
		snapshot, err = v.reader.Capture(ctx, s, e.DocumentID)
		if err != nil {
			return nil, ErrEvidence
		}
		actual = snapshot.HeaderFields[e.Field]
	} else {
		if !validID(e.ChunkID) {
			return nil, ErrEvidence
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		query := url.Values{"view": {"components-v1"}, "chunk_ids": {e.ChunkID}}
		err := v.reader.client.JSON(fetchCtx, http.MethodGet, "/api/v1/knowledge-bases/"+url.PathEscape(s.KnowledgeBaseID)+"/knowledge/"+url.PathEscape(e.DocumentID)+"/source-snapshot", query, nil, &env)
		if err != nil || len(env.Data) > 2<<20 {
			return nil, ErrEvidence
		}
		var components Components
		dec := json.NewDecoder(bytes.NewReader(env.Data))
		dec.DisallowUnknownFields()
		if dec.Decode(&components) != nil || dec.Decode(new(any)) != io.EOF || components.validate(s, e.DocumentID, e.ChunkID) != nil {
			return nil, ErrEvidence
		}
		snapshot = components.Snapshot
		actual = components.ChunkFields[e.ChunkID][e.Field]
	}
	if !validHash(actual) || actual != e.ValueHash {
		return nil, ErrEvidence
	}
	// The current authority observation must be the same complete root as the
	// consumed field proof. A concurrent native change cannot be silently relabeled.
	version, err := v.registry.Refresh(ctx, s, e.DocumentID)
	if err != nil || version.RootHash != snapshot.RootHash {
		return nil, ErrEvidence
	}
	return []provenance.SourceRef{{DocumentID: e.DocumentID, VersionID: version.ID}}, nil
}
