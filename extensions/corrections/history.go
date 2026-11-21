package corrections

import (
	"context"
	"database/sql"
	"time"
)

// HistoryEvent is a retained governance decision without its private payload.
type HistoryEvent struct {
	Sequence    int64     `json:"sequence"`
	OperationID string    `json:"operation_id"`
	ActorID     string    `json:"actor_id"`
	Action      string    `json:"action"`
	Revision    int64     `json:"revision"`
	Status      string    `json:"status"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// HistoryReport separates the current derived state from recorded decisions.
type HistoryReport struct {
	Identity
	State      string         `json:"current_state"`
	ObservedAt time.Time      `json:"observed_at"`
	Events     []HistoryEvent `json:"events"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	HasMore    bool           `json:"has_more"`
}

// History reports decisions for an exact tenant, library, document version and correction.
// The underlying evidence predicates describe a current read, not a past snapshot.
func (s *Service) History(ctx context.Context, scope Scope, doc, version, id string, page, pageSize int) (HistoryReport, error) {
	if s == nil || s.db == nil {
		return HistoryReport{}, ErrUnavailable
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if scope.TenantID == 0 || !ValidID(scope.KnowledgeBaseID) || !ValidID(doc) || !ValidID(version) || !ValidID(id) ||
		page < 1 || page > 10000 || pageSize < 1 || pageSize > 100 {
		return HistoryReport{}, ErrInvalid
	}
	out := HistoryReport{ObservedAt: s.now().UTC(), Events: []HistoryEvent{}, Page: page, PageSize: pageSize}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		entries, err := readTx(ctx, tx, scope, out.ObservedAt, " AND p.document_id=? AND p.version_id=? AND p.feedback_id=?", doc, version, id)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return ErrNotFound
		}
		out.Identity, out.State = entries[0].Identity, entries[0].State
		rows, err := tx.QueryContext(ctx, `SELECT sequence,operation_id,actor_id,action,revision,status,occurred_at
 FROM ext_business_audit WHERE tenant_id=? AND knowledge_base_id=? AND aggregate_id=?
 AND (substr(action,1,9)='feedback_' OR action IN ('qa_feedback_submit','qa_feedback_review'))
 ORDER BY sequence LIMIT ? OFFSET ?`, append(scopeArgs(scope), id, pageSize+1, (page-1)*pageSize)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event HistoryEvent
			var occurredAt string
			if err := rows.Scan(&event.Sequence, &event.OperationID, &event.ActorID, &event.Action, &event.Revision, &event.Status, &occurredAt); err != nil {
				return err
			}
			event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
			if err != nil {
				return err
			}
			out.Events = append(out.Events, event)
		}
		return rows.Err()
	})
	if len(out.Events) > pageSize {
		out.HasMore = true
		out.Events = out.Events[:pageSize]
	}
	return out, err
}
