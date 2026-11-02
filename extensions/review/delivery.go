package review

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

// Handler reconciles the latest deadline/generation under the shared writer.
// Notify is a fast local signal: it must honor ctx, never perform network I/O
// or recursively write the DB. A crash after notify but before commit can repeat
// the signal; the inbox/intent stay durable and logically deduplicated.
func (s *Service) Handler(notify func(context.Context, Alert) error) func(context.Context, outbox.Item) error {
	return func(ctx context.Context, item outbox.Item) error {
		if notify == nil || item.ProjectionType != ProjectionType || item.AggregateType != "document" || item.CausationType != ProjectionType || len(item.Payload) > 4096 {
			return errors.New("invalid review delivery configuration or intent")
		}
		var intent intent
		decoder := json.NewDecoder(bytes.NewReader(item.Payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&intent) != nil || decoder.Decode(new(any)) != io.EOF || intent.AlertID == "" || intent.Generation < 1 || item.ID != queueID(intent.AlertID, intent.Generation) || item.CausationID != cause(intent.AlertID, intent.Generation) {
			return errors.New("invalid review delivery identity")
		}
		scope := lifecycle.Scope{TenantID: item.TenantID, KnowledgeBaseID: item.KnowledgeBaseID, DocumentID: item.AggregateID}
		if err := validateScope(scope); err != nil {
			return err
		}
		return s.db.Write(ctx, func(tx *sql.Tx) error {
			a := Alert{AlertID: intent.AlertID, TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: scope.DocumentID}
			var status, due, detected string
			var delivered sql.NullString
			args := append([]any{intent.AlertID}, scopeArgs(scope)...)
			err := tx.QueryRowContext(ctx, `SELECT review_due,annotation_revision,generation,status,detected_at,delivered_at FROM ext_review_alerts WHERE alert_id=? AND tenant_id=? AND knowledge_base_id=? AND document_id=?`, args...).Scan(&due, &a.AnnotationRevision, &a.Generation, &status, &detected, &delivered)
			if err != nil {
				return err
			}
			if a.Generation != intent.Generation || status != "open" || delivered.Valid {
				return nil
			}
			a.ReviewDue, err = time.Parse(time.RFC3339Nano, due)
			if err != nil {
				return err
			}
			a.DetectedAt, err = time.Parse(time.RFC3339Nano, detected)
			if err != nil {
				return err
			}
			_, currentDue, err := deadlineTx(ctx, tx, scope)
			if err != nil {
				return err
			}
			now := s.now().UTC()
			if currentDue == nil || !currentDue.Equal(a.ReviewDue) || !now.After(*currentDue) {
				_, err = tx.ExecContext(ctx, "UPDATE ext_review_alerts SET status='resolved',resolved_at=? WHERE alert_id=?", now.Format(time.RFC3339Nano), a.AlertID)
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = notify(ctx, a); err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "UPDATE ext_review_alerts SET delivered_at=? WHERE alert_id=? AND generation=?", now.Format(time.RFC3339Nano), a.AlertID, a.Generation)
			return err
		})
	}
}
