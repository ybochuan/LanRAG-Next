package bridge

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ybochuan/LanRAG-Next/extensions/attachmentcleanup"
)

// AttachmentDeleteHandler accepts only the native worker's authenticated
// intent request and acknowledges it after the extension transaction commits.
type AttachmentDeleteHandler struct {
	Key     string
	Cleanup *attachmentcleanup.Service
}

func (h *AttachmentDeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(h.Key) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Key)) != 1 {
		writeError(w, http.StatusUnauthorized, "attachment_delete_unauthorized")
		return
	}
	var req struct {
		RequestID string                     `json:"request_id"`
		TenantID  uint64                     `json:"tenant_id"`
		Targets   []attachmentcleanup.Target `json:"targets"`
		CheckOnly bool                       `json:"check_only,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF || len(req.RequestID) != 36 || len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_attachment_delete")
		return
	}
	action := h.Cleanup.Prepare
	if req.CheckOnly {
		action = h.Cleanup.Validate
	}
	switch err := action(r.Context(), req.TenantID, req.Targets); {
	case errors.Is(err, attachmentcleanup.ErrScope):
		writeError(w, http.StatusForbidden, "attachment_delete_scope_denied")
	case errors.Is(err, attachmentcleanup.ErrConflict):
		writeError(w, http.StatusConflict, "attachment_delete_identity_unresolved")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "attachment_delete_unavailable")
	default:
		w.Header().Set("X-AgentRAG-Request-ID", req.RequestID)
		w.WriteHeader(http.StatusNoContent)
	}
}
