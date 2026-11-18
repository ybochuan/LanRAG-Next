package management

import (
	"context"
	"strings"

	"github.com/ybochuan/LanRAG-Next/extensions/business"
	"github.com/ybochuan/LanRAG-Next/extensions/cleanup"
)

func (s *Server) executeCleanup(ctx context.Context, r Request, id string, payload business.Payload) (any, error) {
	if s.cleanup == nil {
		return nil, business.ErrUnavailable
	}
	scope := cleanup.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}
	switch strings.TrimPrefix(r.Action, "business_") {
	case "cleanup_list":
		return s.cleanup.List(ctx, scope, r.State, r.Page, r.PageSize)
	case "cleanup_hold":
		return s.cleanup.Hold(ctx, scope, id, payload.HoldDays, payload.Reason, r.ActorID, r.OperationID, r.ExpectedRevision)
	case "cleanup_confirm":
		return s.cleanup.Confirm(ctx, scope, id, r.ActorID, r.OperationID, r.ExpectedRevision)
	default:
		return nil, business.ErrInvalid
	}
}

func (s *Server) cleanupSummary(ctx context.Context, r Request) any {
	if s.cleanup == nil {
		return nil
	}
	summary, err := s.cleanup.Summary(ctx, cleanup.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID})
	if err != nil {
		return nil
	}
	return summary
}
