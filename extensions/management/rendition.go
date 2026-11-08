package management

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

// The shared management credential authenticates the caller which vouched for
// CanWrite. A future Core facade must check the original-file download grant.
func (s *Server) serveRendition(w http.ResponseWriter, r *http.Request, req Request) {
	if !req.CanWrite {
		writeError(w, http.StatusForbidden, "original_download_denied")
		return
	}
	if s.renditions == nil {
		writeError(w, http.StatusServiceUnavailable, "rendition_not_configured")
		return
	}
	scope := provenance.Scope{TenantID: req.TenantID, KnowledgeBaseID: req.KnowledgeBaseID}
	if req.Action == "rendition_bind" {
		binding, err := s.renditions.Bind(r.Context(), scope, req.DocumentID, req.ArtifactID)
		if err != nil {
			writeRenditionError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "tenant_id": req.TenantID, "knowledge_base_id": req.KnowledgeBaseID, "data": binding})
		return
	}
	_, body, err := s.renditions.Read(r.Context(), scope, req.DocumentID, req.ArtifactID)
	if err != nil {
		writeRenditionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="middle.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func writeRenditionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lineage.ErrEvidence):
		writeError(w, http.StatusNotFound, "rendition_evidence_unavailable")
	case errors.Is(err, lineage.ErrConflict):
		writeError(w, http.StatusConflict, "rendition_source_unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "rendition_unavailable")
	}
}
