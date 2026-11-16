package review

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

// ErrAlertNotFound reports an acknowledgement for an alert the scope does not hold.
var ErrAlertNotFound = errors.New("review alert not found")

// Acknowledge records that a person read one open reminder in the message
// centre. It is idempotent: acknowledging an already-read alert succeeds
// without moving the original timestamp, so a repeated click or a retried
// request cannot rewrite when the person first saw it.
func (s *Service) Acknowledge(ctx context.Context, scope outbox.Scope, alertID string) error {
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" || alertID == "" {
		return errors.New("explicit inbox scope and alert id required")
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var acknowledged sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT status,acknowledged_at FROM ext_review_alerts WHERE tenant_id=? AND knowledge_base_id=? AND alert_id=?",
			strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, alertID).Scan(&status, &acknowledged)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAlertNotFound
		}
		if err != nil {
			return err
		}
		if status != "open" || acknowledged.Valid {
			// Resolved alerts are not messages any more, and an already-read
			// alert keeps the time the person actually read it.
			return nil
		}
		_, err = tx.ExecContext(ctx, "UPDATE ext_review_alerts SET acknowledged_at=? WHERE tenant_id=? AND knowledge_base_id=? AND alert_id=? AND status='open' AND acknowledged_at IS NULL",
			s.now().UTC().Format(time.RFC3339Nano), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, alertID)
		return err
	})
}
