package bridge

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
)

// CorrectionSourcesHandler accepts only native calls in configured scopes.
type CorrectionSourcesHandler struct {
	Key                     string
	TenantID                uint64
	KnowledgeBaseIDs        []string
	EnabledKnowledgeBaseIDs []string
	Corrections             *corrections.Service
}

func (h *CorrectionSourcesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || len(h.Key) < 32 || h.Corrections == nil {
		writeError(w, 503, "correction_sources_unavailable")
		return
	}
	if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Key)) != 1 {
		writeError(w, 401, "correction_sources_unauthorized")
		return
	}
	var req struct {
		TenantID        uint64   `json:"tenant_id"`
		KnowledgeBaseID string   `json:"knowledge_base_id"`
		KnowledgeIDs    []string `json:"knowledge_ids"`
		Query           string   `json:"query"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF || req.TenantID == 0 || !corrections.ValidID(req.KnowledgeBaseID) {
		writeError(w, 400, "invalid_correction_sources_request")
		return
	}
	if req.TenantID != h.TenantID || !slices.Contains(h.KnowledgeBaseIDs, req.KnowledgeBaseID) {
		writeError(w, 403, "correction_scope_not_configured")
		return
	}
	ids := req.KnowledgeIDs
	if h.EnabledKnowledgeBaseIDs != nil && !slices.Contains(h.EnabledKnowledgeBaseIDs, req.KnowledgeBaseID) {
		ids = nil
	}
	out, err := h.Corrections.Sources(r.Context(), corrections.Scope{TenantID: req.TenantID, KnowledgeBaseID: req.KnowledgeBaseID}, ids, req.Query)
	if errors.Is(err, corrections.ErrInvalid) {
		writeError(w, 400, "invalid_correction_sources_request")
		return
	}
	if err != nil {
		writeError(w, 503, "correction_sources_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Sources []corrections.Source `json:"sources"`
	}{out})
}
