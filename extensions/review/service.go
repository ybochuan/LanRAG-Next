// Package review implements local review-deadline alerts independently of lifecycle state.
package review

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// ProjectionType distinguishes review notifications from native tag writes.
const ProjectionType = "review_alert"

// SignalCode is the only data emitted to the Host's general log reporter.
const SignalCode = "document_review_overdue"

// Alert is one scoped inbox item. Generation increases only when a resolved
// deadline becomes overdue again; changing unrelated labels does not resend it.
type Alert struct {
	AlertID         string    `json:"alert_id"`
	TenantID        uint64    `json:"tenant_id"`
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	DocumentID      string    `json:"document_id"`
	ReviewDue       time.Time `json:"review_due"`
	// AnnotationRevision is the local annotation revision, or the declaration
	// revision when the effective deadline is inherited from a parent.
	AnnotationRevision int64      `json:"annotation_revision"`
	Generation         int64      `json:"generation"`
	DetectedAt         time.Time  `json:"detected_at"`
	DeliveredAt        *time.Time `json:"delivered_at,omitempty"`
	DeliveryState      string     `json:"delivery_state"`
}

// Service shares the extension single writer and never owns the DB lifetime.
type Service struct {
	db  *store.DB
	now func() time.Time
}

// NewService accepts an injected UTC clock; nil uses the system clock.
func NewService(db *store.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}
func scopeArgs(s lifecycle.Scope) []any {
	return []any{strconv.FormatUint(s.TenantID, 10), s.KnowledgeBaseID, s.DocumentID}
}
func validateScope(s lifecycle.Scope) error {
	if s.TenantID == 0 || s.KnowledgeBaseID == "" || s.DocumentID == "" {
		return errors.New("explicit review document scope required")
	}
	return nil
}
func deadlineTx(ctx context.Context, tx *sql.Tx, scope lifecycle.Scope) (int64, *time.Time, error) {
	var revision int64
	var raw sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT revision,review_due FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, scopeArgs(scope)...).Scan(&revision, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, nil, err
	}
	var due *time.Time
	if raw.Valid {
		parsed, e := time.Parse(time.RFC3339Nano, raw.String)
		if e != nil {
			return 0, nil, e
		}
		parsed = parsed.UTC()
		due = &parsed
	}
	// Read-only schema5..8 inbox queries intentionally do not migrate tables.
	var supported bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='ext_supplement_current')").Scan(&supported); err != nil {
		return 0, nil, err
	}
	if supported {
		a, e := dependencies.LoadTx(ctx, tx, dependencies.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: scope.DocumentID})
		if e != nil {
			return 0, nil, e
		}
		if a.ReviewDue != nil && (due == nil || a.ReviewDue.Before(*due)) {
			due = a.ReviewDue
			revision = a.Revision
		}
	}
	return revision, due, nil
}
func alertID(scope lifecycle.Scope, due string) string {
	body, _ := json.Marshal([]string{strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, scope.DocumentID, due})
	return fmt.Sprintf("%x", sha256.Sum256(body))
}
func cause(id string, generation int64) string   { return id + ":" + strconv.FormatInt(generation, 10) }
func queueID(id string, generation int64) string { return "review:" + cause(id, generation) }

type intent struct {
	AlertID    string `json:"alert_id"`
	Generation int64  `json:"generation"`
}

// Check commits the current inbox decision and its notification intent together.
// No lifecycle event, head, state, label or upstream API is changed.
func (s *Service) Check(ctx context.Context, scope lifecycle.Scope) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	if s.db == nil {
		return errors.New("review database required")
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		revision, due, err := deadlineTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if now.IsZero() {
			return errors.New("valid review clock required")
		}
		eligible := due != nil && now.After(*due)
		rawDue := ""
		if due != nil {
			rawDue = due.Format(time.RFC3339Nano)
		}
		closeAll := !eligible
		args := append([]any{now.Format(time.RFC3339Nano)}, scopeArgs(scope)...)
		args = append(args, closeAll, rawDue)
		if _, err = tx.ExecContext(ctx, `UPDATE ext_review_alerts SET status='resolved',resolved_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status='open' AND (? OR review_due<>?)`, args...); err != nil {
			return err
		}
		if !eligible {
			return nil
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO ext_review_subjects VALUES(?,?,?) ON CONFLICT DO NOTHING", scopeArgs(scope)...); err != nil {
			return err
		}
		id := alertID(scope, rawDue)
		var generation int64
		var status string
		err = tx.QueryRowContext(ctx, "SELECT generation,status FROM ext_review_alerts WHERE alert_id=?", id).Scan(&generation, &status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && status == "open" {
			return nil
		}
		generation++
		args = append([]any{id}, scopeArgs(scope)...)
		args = append(args, rawDue, revision, generation, now.Format(time.RFC3339Nano))
		_, err = tx.ExecContext(ctx, `INSERT INTO ext_review_alerts(alert_id,tenant_id,knowledge_base_id,document_id,review_due,annotation_revision,generation,status,detected_at) VALUES(?,?,?,?,?,?,?,'open',?) ON CONFLICT(alert_id) DO UPDATE SET annotation_revision=excluded.annotation_revision,generation=excluded.generation,status='open',detected_at=excluded.detected_at,delivered_at=NULL,resolved_at=NULL`, args...)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(intent{id, generation})
		if err != nil {
			return err
		}
		return outbox.EnqueueTx(ctx, tx, outbox.Item{ID: queueID(id, generation), TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, ProjectionType: ProjectionType, AggregateType: "document", AggregateID: scope.DocumentID, CausationType: ProjectionType, CausationID: cause(id, generation), Payload: payload, CreatedAt: now})
	})
}

// Sweep covers all annotations in bounded pages within explicit configured KBs.
func (s *Service) Sweep(ctx context.Context, scopes []lifecycle.PolicyScope) error {
	if len(scopes) == 0 || len(scopes) > 1000 {
		return errors.New("explicit review scan scopes required")
	}
	for _, scope := range scopes {
		if scope.TenantID == 0 || scope.KnowledgeBaseID == "" {
			return errors.New("explicit review tenant/KB required")
		}
	}
	var failures []error
	for _, scope := range scopes {
		cursor := ""
		for {
			var ids []string
			err := s.db.View(ctx, func(tx *sql.Tx) error {
				rows, err := tx.QueryContext(ctx, `SELECT document_id FROM (SELECT document_id FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? UNION SELECT document_id FROM ext_supplement_current WHERE tenant_id=? AND knowledge_base_id=?) WHERE document_id>? ORDER BY document_id LIMIT 256`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, cursor)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var id string
					if err = rows.Scan(&id); err != nil {
						return err
					}
					ids = append(ids, id)
				}
				return rows.Err()
			})
			if err != nil {
				return err
			}
			for _, id := range ids {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err = s.Check(ctx, lifecycle.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: id}); err != nil {
					if len(failures) < 16 {
						failures = append(failures, err)
					}
				}
			}
			if len(ids) < 256 {
				break
			}
			cursor = ids[len(ids)-1]
		}
	}
	return errors.Join(failures...)
}

// RunDaily scans at startup and every 24 hours, reporting failures for retry.
func (s *Service) RunDaily(ctx context.Context, scopes []lifecycle.PolicyScope, report func(error)) error {
	if report == nil {
		return errors.New("review scan reporter required")
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if err := s.Sweep(ctx, scopes); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			report(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
