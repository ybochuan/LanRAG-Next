package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func lifecycleError(c *gin.Context, status int) {
	code := "lifecycle_unavailable"
	switch status {
	case 400:
		code = "invalid_configuration"
	case 401:
		code = "authentication_required"
	case 403:
		code = "read_only"
	case 404:
		code = "not_found"
	case 409:
		code = "revision_conflict"
	}
	c.JSON(status, gin.H{"success": false, "error": gin.H{"code": code, "message": code}})
}

func lifecycleCanWrite(caller types.Caller, kb *types.KnowledgeBase, permission types.OrgMemberRole) bool {
	if !caller.Role.HasPermission(types.TenantRoleContributor) {
		return false
	}
	if caller.TenantID != kb.TenantID {
		return permission.HasPermission(types.OrgRoleEditor)
	}
	_, err := access.CheckOwnershipOrRole(access.OwnershipRequest{UserID: caller.UserID, Role: caller.Role, Enforce: true}, types.TenantRoleAdmin, func() (string, error) { return kb.CreatorID, nil })
	return err == nil
}

// ExtensionLifecycle is a JWT-only authorization/DTO facade; all lifecycle business rules remain in extensions.
func (h *KnowledgeBaseHandler) ExtensionLifecycle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx := c.Request.Context()
	caller := types.CallerFromContext(ctx)
	_, machine := types.TenantAPIKeyScopeFromContext(ctx)
	_, auditKey := types.AuditAPIKeyFromContext(ctx)
	if machine || auditKey {
		lifecycleError(c, 403)
		return
	}
	if caller.UserID == "" || caller.TenantID == 0 {
		lifecycleError(c, 401)
		return
	}
	grant, err := resolveHandlerKBAccess(c, c.Param("id"), h.service, h.kbShareService, h.agentShareService)
	if err != nil {
		_ = c.Error(err)
		return
	}
	canWrite := lifecycleCanWrite(caller, grant.KnowledgeBase, grant.Permission)
	mutation := c.Request.Method == http.MethodPut
	if mutation && !canWrite {
		lifecycleError(c, 403)
		return
	}
	if mutation {
		grant, err = resolveHandlerKBAccessFor(c, c.Param("id"), h.service, h.kbShareService, h.agentShareService, types.OrgRoleEditor)
		if err != nil {
			_ = c.Error(err)
			return
		}
	}
	action := "overview"
	switch {
	case c.Param("business_action") != "":
		action = "business_" + c.Param("business_action")
		read := false
		switch action {
		case "business_lineage_graph", "business_related", "business_relation_list", "business_feedback_list",
			"business_guidance_list", "business_revision_list", "business_family_list", "business_family_members",
			"business_family_workspace", "business_family_replacements", "business_family_suggestions", "business_cleanup_list",
			"business_attachment_list", "business_attachment_preview", "business_attachment_tasks", "business_correction_documents", "business_correction_document",
			"business_correction_markdown", "business_correction_history":
			read = true
		}
		if read == mutation {
			lifecycleError(c, 400)
			return
		}
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/documents/:document_id/supplements":
		action = "supplements"
		if mutation {
			action = "put_supplements"
		}
	case c.Param("document_id") != "":
		action = "document"
		if mutation {
			action = "put_annotations"
		}
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/governance-health":
		action = "governance_health"
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/governance-health/retry":
		action = "retry_blocked"
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/rules":
		action = "rules"
		if mutation {
			action = "put_rules"
		}
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/reviews":
		action = "reviews"
	case c.FullPath() == "/api/v1/knowledge-bases/:id/lifecycle/authority":
		action = "authority"
	}
	req := extensionbridge.ManagementRequest{Action: action, TenantID: grant.EffectiveTenantID, KnowledgeBaseID: grant.KnowledgeBase.ID, DocumentID: c.Param("document_id"), CanWrite: canWrite, Page: 1, PageSize: 20, State: c.Query("state")}
	if action == "business_lineage_graph" {
		req.GraphMode = c.Query("mode")
	}
	if action == "overview" {
		req.Title = c.Query("title")
	}
	if action == "business_related" || strings.HasPrefix(action, "business_attachment_") {
		req.DocumentID = c.Query("document_id")
	}
	if strings.HasPrefix(action, "business_correction_") {
		if !canWrite && (action == "business_correction_history" || req.State != "" && req.State != "active") {
			lifecycleError(c, 403)
			return
		}
		req.DocumentID = c.Query("document_id")
		req.VersionID = c.Query("version_id")
		req.Query = c.Query("query")
		req.ApprovedFrom = c.Query("approved_from")
		req.ApprovedTo = c.Query("approved_to")
		if action != "business_correction_documents" && (req.DocumentID == "" || req.VersionID == "") {
			lifecycleError(c, 400)
			return
		}
		if action == "business_correction_history" {
			req.Business, _ = json.Marshal(map[string]string{"id": c.Query("id")})
		}
	} else if strings.HasPrefix(action, "business_") && !mutation {
		req.Business, _ = json.Marshal(map[string]string{"id": c.Query("id")})
	}
	for name, target := range map[string]*int{"page": &req.Page, "page_size": &req.PageSize} {
		if value := c.Query(name); value != "" {
			number, e := strconv.Atoi(value)
			if e != nil {
				lifecycleError(c, 400)
				return
			}
			*target = number
		}
	}
	if req.DocumentID != "" {
		doc, e := h.knowledgeService.GetKnowledgeByID(grant.Context(ctx), req.DocumentID)
		if e != nil || doc == nil || doc.KnowledgeBaseID != req.KnowledgeBaseID || doc.TenantID != req.TenantID {
			lifecycleError(c, 404)
			return
		}
	}
	if mutation {
		if action == "business_attachment_upload" {
			data, err := extensionbridge.UploadWorkbenchAttachment(ctx, req, c.Query("parent_version"), c.Query("file_name"),
				http.MaxBytesReader(c.Writer, c.Request.Body, 20<<20))
			if err != nil {
				lifecycleError(c, extensionbridge.ManagementStatus(err))
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
			return
		}
		var body struct {
			ExpectedRevision       *int64          `json:"expected_revision"`
			Annotations            json.RawMessage `json:"annotations"`
			Rules                  json.RawMessage `json:"rules"`
			OperationID            string          `json:"operation_id"`
			Parents                *[]string       `json:"parents"`
			ParentKnowledgeBaseIDs *[]string       `json:"parent_knowledge_base_ids"`
			Business               json.RawMessage `json:"business"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
		dec.DisallowUnknownFields()
		if e := dec.Decode(&body); e != nil || dec.Decode(new(any)) != io.EOF || body.ExpectedRevision == nil || *body.ExpectedRevision < 0 {
			lifecycleError(c, 400)
			return
		}
		req.ExpectedRevision = *body.ExpectedRevision
		req.Annotations = body.Annotations
		req.Rules = body.Rules
		if action == "put_rules" && (len(body.Rules) == 0 || len(body.Annotations) > 0) || action == "put_annotations" && (len(body.Annotations) == 0 || len(body.Rules) > 0) {
			lifecycleError(c, 400)
			return
		}
		if strings.HasPrefix(action, "business_") {
			if len(body.Business) == 0 || body.OperationID == "" || len(body.Annotations) > 0 || len(body.Rules) > 0 || body.Parents != nil || body.ParentKnowledgeBaseIDs != nil {
				lifecycleError(c, 400)
				return
			}
			req.ActorID = strconv.FormatUint(caller.TenantID, 10) + ":" + caller.UserID
			req.OperationID = body.OperationID
			req.Business = body.Business
		} else if action == "put_supplements" {
			if body.Parents == nil || len(*body.Parents) > 8 || body.OperationID == "" || len(body.OperationID) > 128 || len(body.Annotations) > 0 || len(body.Rules) > 0 {
				lifecycleError(c, 400)
				return
			}
			parentKBs := make([]string, len(*body.Parents))
			for i := range parentKBs {
				parentKBs[i] = req.KnowledgeBaseID
			}
			if body.ParentKnowledgeBaseIDs != nil {
				if len(*body.ParentKnowledgeBaseIDs) != len(*body.Parents) {
					lifecycleError(c, 400)
					return
				}
				for i, kb := range *body.ParentKnowledgeBaseIDs {
					if kb == "" || len(kb) > 128 {
						lifecycleError(c, 400)
						return
					}
					parentKBs[i] = kb
				}
			}
			for i, id := range *body.Parents {
				if id == "" || len(id) > 128 {
					lifecycleError(c, 400)
					return
				}
				parent, e := h.knowledgeService.GetKnowledgeByID(grant.Context(ctx), id)
				if e != nil || parent == nil || parent.ID != id || parent.KnowledgeBaseID != parentKBs[i] || parent.TenantID != req.TenantID {
					lifecycleError(c, 404)
					return
				}
			}
			req.ActorID = strconv.FormatUint(caller.TenantID, 10) + ":" + caller.UserID
			req.OperationID = body.OperationID
			req.Parents = *body.Parents
			req.ParentKnowledgeBaseIDs = parentKBs
		} else if body.Parents != nil || body.ParentKnowledgeBaseIDs != nil || body.OperationID != "" {
			lifecycleError(c, 400)
			return
		}
	}
	data, err := extensionbridge.ManageLifecycle(ctx, req)
	if err != nil {
		lifecycleError(c, extensionbridge.ManagementStatus(err))
		return
	}
	c.JSON(200, gin.H{"success": true, "data": data})
}
