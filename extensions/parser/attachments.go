package parser

import (
	"io"
	"mime"
	"net/http"
)

func (h *NativeHandler) extractAttachments(w http.ResponseWriter, r *http.Request, id nativeIdentity) {
	if r.Method != "POST" {
		proxyError(w, 405, "method_not_allowed")
		return
	}
	if h.cfg.Attachments == nil {
		proxyError(w, 503, "attachment_import_unavailable")
		return
	}
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition"))
	name := params["filename"]
	if err != nil || name == "" || len(name) > 256 {
		proxyError(w, 400, "invalid_attachment_filename")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, proxySourceLimit+1))
	if err != nil || len(data) == 0 || len(data) > proxySourceLimit {
		proxyError(w, 400, "invalid_attachment_source")
		return
	}
	receipts, err := h.cfg.Attachments.Process(r.Context(), id.TenantID, id.KnowledgeBaseID, id.DocID, name, data)
	if err != nil {
		proxyError(w, 503, "attachment_import_failed")
		return
	}
	if len(receipts) > 0 && h.cfg.Report != nil {
		h.cfg.Report("parser_attachments_imported")
	}
	proxyJSON(w, 200, map[string]any{"status": "ok", "attachments": receipts})
}
