package management

import (
	"context"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
)

// notificationAck marks one message read in the message centre. The alert must
// belong to the named scope, so a caller cannot acknowledge a message in a
// knowledge base it was not authorized for by naming another one.
func (s *Server) notificationAck(ctx context.Context, r Request, alertID string) (any, error) {
	if _, ok := s.scopes[lifecycle.PolicyScope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}]; !ok {
		return nil, errScopeNotConfigured
	}
	err := review.NewService(s.db, time.Now).Acknowledge(ctx, outbox.Scope{TenantID: r.TenantID, KnowledgeBaseID: r.KnowledgeBaseID}, alertID)
	if err != nil {
		if errors.Is(err, review.ErrAlertNotFound) {
			return nil, errNotFound
		}
		return nil, errDatabase
	}
	return map[string]any{"alert_id": alertID, "acknowledged": true}, nil
}
