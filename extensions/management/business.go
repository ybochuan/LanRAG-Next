package management

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

const workbenchRelations = "workbench-relations"

type relatedResult struct {
	business.Related
	RelationID string `json:"relation_id,omitempty"`
}

func businessRead(action string) bool {
	switch action {
	case "business_lineage_graph", "business_related", "business_relation_list", "business_feedback_list",
		"business_guidance_list", "business_revision_list", "business_family_list", "business_family_members",
		"business_family_replacements", "business_family_backfill_plans", "business_family_workspace",
		"business_family_suggestions", "business_cleanup_list", "business_attachment_list", "business_attachment_preview",
		"business_correction_documents", "business_correction_document":
		return true
	default:
		return false
	}
}
func (s *Server) executeBusiness(ctx context.Context, r Request) (any, error) {
	if r.Action == "business_correction_documents" || r.Action == "business_correction_document" {
		if !r.CanWrite && r.State != "" && r.State != "active" {
			return nil, errScopeNotConfigured
		}
		scope := corrections.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}
		filter := corrections.Filter{DocumentID: r.DocumentID, VersionID: r.VersionID, Query: r.Query, State: r.State, ApprovedFrom: r.ApprovedFrom, ApprovedTo: r.ApprovedTo, Page: r.Page, PageSize: r.PageSize, History: r.CanWrite}
		if r.Action == "business_correction_document" {
			return s.corrections.Document(ctx, scope, filter, s.titles)
		}
		return s.corrections.Documents(ctx, scope, filter, s.titles)
	}
	if strings.HasPrefix(r.Action, "business_attachment_") {
		return s.executeAttachment(ctx, r)
	}
	b := s.business
	if b == nil {
		b = business.New(s.db, s.sources, time.Now)
	}
	body, err := decodeBusiness(r.Business)
	if err != nil {
		return nil, err
	}
	if r.Action == "business_lineage_graph" {
		return s.lineageGraph(ctx, r, body.ID)
	}
	// Reading the message centre reuses the per-KB reviews action, so only the
	// acknowledgement needs its own write path here.
	if r.Action == "business_notification_ack" {
		return s.notificationAck(ctx, r, body.ID)
	}
	if strings.HasPrefix(r.Action, "business_cleanup_") {
		return s.executeCleanup(ctx, r, body.ID, body.Payload)
	}
	if body.Payload.TargetKnowledgeBaseID != "" &&
		!s.scopes[lifecycle.PolicyScope{TenantID: r.TenantID, KnowledgeBaseID: body.Payload.TargetKnowledgeBaseID}] {
		return nil, errScopeNotConfigured
	}
	c := business.Command{Scope: provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, ActorID: r.ActorID, Action: strings.TrimPrefix(r.Action, "business_"), OperationID: r.OperationID, ID: body.ID, ExpectedRevision: r.ExpectedRevision, Payload: body.Payload}
	c.Page = r.Page
	c.PageSize = r.PageSize
	relationList := r.State == workbenchRelations
	if c.Action == "relation_list" || c.Action == "related" {
		if filters, ok := strings.CutPrefix(r.State, workbenchRelations+":"); ok {
			typ, status, ok := strings.Cut(filters, ":")
			if !ok || strings.Contains(status, ":") {
				return nil, business.ErrInvalid
			}
			relationList = true
			c.ListEdgeType, c.ListStatus = typ, status
		}
		c.IncludeSupplementDeclarations = relationList
	}
	switch c.Action {
	case "family_list":
		return b.Families(ctx, c)
	case "family_members":
		return b.FamilyMembers(ctx, c)
	case "family_workspace":
		workspace, err := b.FamilyWorkspace(ctx, c, r.State)
		return struct {
			business.FamilyWorkspace
			CanWrite bool `json:"can_write"`
		}{workspace, r.CanWrite}, err
	case "family_replacements":
		if r.State == "history-preview" {
			return b.FamilyReplacementHistory(ctx, c)
		}
		c.ListStatus = r.State
		return b.FamilyReplacements(ctx, c)
	case "family_suggestions":
		c.Payload.DocumentID = r.State
		return b.FamilySuggestions(ctx, c)
	case "family_backfill_plans":
		return b.FamilyBackfillPlans(ctx, c)
	case "rendition_bind":
		return s.renditions.Bind(ctx, c.Scope, c.Payload.DocumentID, c.ID)
	case "related":
		if r.State == "workbench-activity" {
			c.Payload.DocumentID = r.DocumentID
			return b.DocumentActivity(ctx, c)
		}
		if relationList || r.State == "workbench-revisions" {
			if r.DocumentID == "" {
				return nil, business.ErrInvalid
			}
			c.Payload.DocumentID = r.DocumentID
			c.Action = "relation_list"
			if r.State == "workbench-revisions" {
				c.Action = "revision_list"
			}
			return b.List(ctx, c)
		}
		rows, err := b.QueryRelated(ctx, c.Scope, r.DocumentID)
		if err != nil {
			return nil, err
		}
		out := make([]relatedResult, 0, len(rows))
		for _, row := range rows {
			if s.scopes[lifecycle.PolicyScope{TenantID: r.TenantID, KnowledgeBaseID: row.KnowledgeBaseID}] {
				out = append(out, relatedResult{Related: row, RelationID: row.RelationID})
			}
		}
		return out, nil
	case "relation_list", "feedback_list", "guidance_list", "revision_list":
		if c.Action == "feedback_list" {
			c.ListStatus = r.State
			c.IncludeFeedbackContext = r.CanWrite
		}
		return b.List(ctx, c)
	case "revision_publish":
		return b.Publish(ctx, c.Scope, c.ID)
	default:
		return b.Execute(ctx, c)
	}
}

type businessBody struct {
	ID      string           `json:"id"`
	Payload business.Payload `json:"payload"`
}

func decodeBusiness(raw json.RawMessage) (businessBody, error) {
	var body businessBody
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF {
			return body, business.ErrInvalid
		}
	}
	return body, nil
}

func (s *Server) feedbackTenantConfigured(tenant uint64) bool {
	for scope := range s.scopes {
		if scope.TenantID == tenant {
			return true
		}
	}
	return false
}

func (s *Server) executeQAFeedback(ctx context.Context, r Request, configured bool) (any, error) {
	body, err := decodeBusiness(r.Business)
	if err != nil {
		return nil, err
	}
	b := s.business
	if b == nil {
		b = business.New(s.db, s.sources, time.Now)
	}
	c := business.Command{Scope: provenance.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID},
		Action: r.Action, ActorID: r.ActorID, OperationID: r.OperationID, ID: body.ID, ExpectedRevision: r.ExpectedRevision, Payload: body.Payload}
	switch r.Action {
	case "qa_feedback_submit":
		return b.SubmitQAFeedback(ctx, c, configured)
	case "qa_feedback_list":
		c.Page, c.PageSize, c.ListStatus, c.IncludeFeedbackContext = r.Page, r.PageSize, r.State, r.CanWrite
		return b.List(ctx, c)
	case "qa_feedback_review":
		return b.ReviewQAInbox(ctx, c)
	default:
		return nil, business.ErrInvalid
	}
}
