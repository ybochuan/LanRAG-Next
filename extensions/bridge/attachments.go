package bridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"io"
	"net/http"
)

// AttachmentPolicy resolves related downloads under current lifecycle permits.
type AttachmentPolicy interface {
	AttachmentLinks(context.Context, contract.BeforeRetrieveRequest) (map[string][]contract.RelatedAttachment, error)
}

// AttachmentHandler serves the optional, authenticated after-retrieve enrichment.
type AttachmentHandler struct {
	Key    string
	Policy AttachmentPolicy
}

func (h *AttachmentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Policy == nil || len(h.Key) < 32 {
		writeError(w, 503, "attachments_unavailable")
		return
	}
	if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Key)) != 1 {
		writeError(w, 401, "attachments_unauthorized")
		return
	}
	data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, contract.MaxRequestBytes))
	if e != nil {
		writeError(w, 400, "invalid_attachment_request")
		return
	}
	req, e := contract.DecodeBeforeRetrieve(data)
	if e != nil {
		writeError(w, 400, "invalid_attachment_request")
		return
	}
	links, e := h.Policy.AttachmentLinks(r.Context(), req)
	if e != nil {
		writeError(w, 503, "attachments_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.AfterRetrieveResponse{RequestID: req.RequestID, TenantID: req.TenantID, KnowledgeBaseIDs: req.KnowledgeBaseIDs, RelatedAttachments: links})
}
