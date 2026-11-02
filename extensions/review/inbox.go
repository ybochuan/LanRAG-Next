package review

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

// List returns currently overdue alerts, including failed/dead deliveries.
// Current annotation dates hide obsolete alerts before the next daily sweep.
func (s *Service) List(ctx context.Context, scope outbox.Scope, limit int) (alerts []Alert, err error) {
	err = s.db.View(ctx, func(tx *sql.Tx) error { alerts, err = ListTx(ctx, tx, scope, limit, s.now().UTC()); return err })
	return
}

// ListTx can be used with a read-only transaction; it never migrates or writes.
// A caller must supply an explicit tenant/KB and a bounded result limit.
func ListTx(ctx context.Context, tx *sql.Tx, scope outbox.Scope, limit int, now time.Time) ([]Alert, error) {
	if scope.TenantID == 0 || scope.KnowledgeBaseID == "" || limit < 1 || limit > 1000 || now.IsZero() {
		return nil, errors.New("explicit inbox scope, limit 1..1000 and clock required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.alert_id,a.document_id,a.review_due,a.annotation_revision,a.generation,a.detected_at,a.delivered_at,COALESCE(o.status,'missing') FROM ext_review_alerts a LEFT JOIN ext_projection_outbox o ON o.outbox_id='review:'||a.alert_id||':'||a.generation WHERE a.tenant_id=? AND a.knowledge_base_id=? AND a.status='open' ORDER BY a.detected_at,a.alert_id`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := make([]Alert, 0)
	for rows.Next() {
		a := Alert{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}
		var due, detected string
		var delivered sql.NullString
		if err = rows.Scan(&a.AlertID, &a.DocumentID, &due, &a.AnnotationRevision, &a.Generation, &detected, &delivered, &a.DeliveryState); err != nil {
			return nil, err
		}
		a.ReviewDue, err = time.Parse(time.RFC3339Nano, due)
		if err != nil {
			return nil, err
		}
		if !now.After(a.ReviewDue) {
			continue
		}
		_, currentDue, e := deadlineTx(ctx, tx, lifecycle.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: a.DocumentID})
		if e != nil {
			return nil, e
		}
		if currentDue == nil || !currentDue.Equal(a.ReviewDue) {
			continue
		}
		a.DetectedAt, err = time.Parse(time.RFC3339Nano, detected)
		if err != nil {
			return nil, err
		}
		if delivered.Valid {
			v, err := time.Parse(time.RFC3339Nano, delivered.String)
			if err != nil {
				return nil, err
			}
			a.DeliveredAt = &v
		}
		alerts = append(alerts, a)
		if len(alerts) == limit {
			break
		}
	}
	return alerts, rows.Err()
}
