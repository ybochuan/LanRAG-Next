package management

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/attachmentworkbench"
	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/parser/attachment"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func (s *Server) executeAttachment(ctx context.Context, r Request) (any, error) {
	if s.attachments == nil {
		return nil, business.ErrUnavailable
	}
	scope := attachmentworkbench.Scope{Tenant: r.TenantID, KB: r.KnowledgeBaseID}
	switch r.Action {
	case "business_attachment_tasks":
		return s.attachments.Tasks(ctx, scope)
	case "business_attachment_list":
		if !weknora.ValidID(r.DocumentID) {
			return nil, business.ErrInvalid
		}
		return s.attachments.List(ctx, scope, r.DocumentID)
	case "business_attachment_preview":
		if !r.CanWrite {
			return nil, errScopeNotConfigured
		}
		if !weknora.ValidID(r.DocumentID) {
			return nil, business.ErrInvalid
		}
		return s.attachments.Preview(ctx, scope, r.DocumentID)
	}
	var c attachmentworkbench.Command
	dec := json.NewDecoder(strings.NewReader(string(r.Business)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil || dec.Decode(new(any)) != io.EOF || !weknora.ValidID(c.Parent) || c.Parent != r.DocumentID || r.ActorID == "" {
		return nil, business.ErrInvalid
	}
	switch r.Action {
	case "business_attachment_import":
		items, err := s.attachments.Import(ctx, scope, c)
		if err != nil {
			return nil, err
		}
		receipts := []attachment.Receipt{}
		partial := false
		for _, item := range items {
			if item.Error != "" {
				partial = true
				continue
			}
			receipts = append(receipts, attachment.Receipt{FileName: item.FileName, DocumentID: item.DocumentID})
		}
		return map[string]any{"items": items, "receipts": receipts, "partial": partial}, nil
	case "business_attachment_confirm", "business_attachment_detach", "business_attachment_restore":
		if !weknora.ValidID(c.Document) {
			return nil, business.ErrInvalid
		}
		return s.attachments.Edit(ctx, scope, strings.TrimPrefix(r.Action, "business_attachment_"), r.ActorID, r.OperationID, r.ExpectedRevision, c)
	default:
		return nil, business.ErrInvalid
	}
}

func (s *Server) attachmentUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tenant, err := strconv.ParseUint(q.Get("tenant_id"), 10, 64)
	kb, parent, version, name := q.Get("knowledge_base_id"), q.Get("document_id"), q.Get("parent_version"), q.Get("file_name")
	if err != nil || !s.scopes[lifecycle.PolicyScope{TenantID: tenant, KnowledgeBaseID: kb}] {
		writeError(w, http.StatusForbidden, "scope_not_configured")
		return
	}
	if s.attachments == nil {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable")
		return
	}
	if !weknora.ValidID(parent) || len(version) != 32 || name == "" || len(name) > 512 {
		writeError(w, http.StatusBadRequest, "invalid_attachment")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, attachment.MaxFileBytes-1))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_attachment")
		return
	}
	receipt, err := s.attachments.Upload(r.Context(), attachmentworkbench.Scope{Tenant: tenant, KB: kb}, parent, version, name, data)
	if err != nil {
		if errors.Is(err, store.ErrAttachmentConflict) {
			writeError(w, http.StatusConflict, "revision_conflict")
		} else {
			writeError(w, http.StatusServiceUnavailable, "management_unavailable")
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "tenant_id": tenant, "knowledge_base_id": kb, "data": receipt})
}
