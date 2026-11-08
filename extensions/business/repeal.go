package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

type repealNotice struct {
	id, document, target, sourceVersion, targetVersion, status string
	due, revision                                              int64
}

func readRepeal(ctx context.Context, tx *sql.Tx, scope provenance.Scope, id string) (repealNotice, error) {
	n := repealNotice{id: id}
	err := tx.QueryRowContext(ctx, `SELECT document_id,target_document_id,source_version_id,target_version_id,effective_at_ms,status,revision
		FROM ext_repeal_notices WHERE tenant_id=? AND knowledge_base_id=? AND notice_id=?`, args(scope, id)...).
		Scan(&n.document, &n.target, &n.sourceVersion, &n.targetVersion, &n.due, &n.status, &n.revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrConflict
	}
	return n, err
}

func (s *Service) repeal(ctx context.Context, c Command) (Result, error) {
	if s == nil || s.db == nil {
		return Result{}, ErrUnavailable
	}
	if c.Scope.TenantID == 0 || !validID(c.Scope.KnowledgeBaseID) || !validID(c.ActorID) || !validID(c.OperationID) || c.ExpectedRevision < 0 {
		return Result{}, ErrInvalid
	}
	if result, replayed, err := s.replay(ctx, c); replayed || err != nil {
		return result, err
	}
	if c.Action == "repeal_create" {
		return s.createRepeal(ctx, c)
	}
	if !validID(c.ID) {
		return Result{}, ErrInvalid
	}
	if c.Action == "repeal_approve" {
		var n repealNotice
		err := s.db.View(ctx, func(tx *sql.Tx) error {
			var err error
			n, err = readRepeal(ctx, tx, c.Scope, c.ID)
			return err
		})
		if err != nil {
			return Result{}, err
		}
		if n.status == "pending_review" && n.revision == c.ExpectedRevision {
			if err := s.verifyRepeal(ctx, c.Scope, n, s.verify); err != nil {
				return s.replayAfterObservationError(ctx, c, err)
			}
		}
	}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		n, err := readRepeal(ctx, tx, c.Scope, c.ID)
		if err != nil {
			return Result{}, err
		}
		if n.revision != c.ExpectedRevision {
			return Result{}, ErrConflict
		}
		if c.Action == "repeal_cancel" {
			if n.status != "pending_review" && n.status != "scheduled" && n.status != "needs_review" {
				return Result{}, ErrConflict
			}
			return updateRepeal(ctx, tx, c.Scope, n, "revoked", nil, nil)
		}
		if n.status != "pending_review" {
			return Result{}, ErrConflict
		}
		if err := s.currentRepeal(ctx, tx, c.Scope, n); err != nil {
			return Result{}, err
		}
		var reserved bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ext_repeal_notices
			WHERE tenant_id=? AND knowledge_base_id=? AND target_document_id=? AND status='scheduled')`,
			args(c.Scope, n.target)...).Scan(&reserved); err != nil {
			return Result{}, err
		}
		if reserved {
			return Result{}, ErrConflict
		}
		if now := s.now().UTC(); n.due <= now.UnixMilli() {
			return applyRepeal(ctx, tx, c.Scope, n, now)
		}
		return updateRepeal(ctx, tx, c.Scope, n, "scheduled", nil, nil)
	})
}

func (s *Service) createRepeal(ctx context.Context, c Command) (Result, error) {
	p := c.Payload
	if !validID(p.DocumentID) || !validID(p.TargetDocumentID) || p.DocumentID == p.TargetDocumentID ||
		p.EffectiveAt == nil || p.EffectiveAt.UnixMilli() <= 0 || c.ExpectedRevision != 0 {
		return Result{}, ErrInvalid
	}
	source, err := s.verify(ctx, c.Scope, p.DocumentID)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	target, err := s.verify(ctx, c.Scope, p.TargetDocumentID)
	if err != nil {
		return s.replayAfterObservationError(ctx, c, err)
	}
	n := repealNotice{document: p.DocumentID, target: p.TargetDocumentID, sourceVersion: source,
		targetVersion: target, due: p.EffectiveAt.UnixMilli()}
	return s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		if err := s.currentRepeal(ctx, tx, c.Scope, n); err != nil {
			return Result{}, err
		}
		out := Result{ID: newID(), Status: "pending_review", Revision: 1}
		if out.ID == "" {
			return Result{}, ErrUnavailable
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO ext_repeal_notices(tenant_id,knowledge_base_id,notice_id,
			document_id,target_document_id,source_version_id,target_version_id,effective_at_ms,status,revision)
			VALUES(?,?,?,?,?,?,?,?,'pending_review',1)`, args(c.Scope, out.ID, n.document, n.target, source, target, n.due)...)
		return out, err
	})
}

func (s *Service) verifyRepeal(ctx context.Context, scope provenance.Scope, n repealNotice,
	verify func(context.Context, provenance.Scope, string) (string, error)) error {
	for _, pin := range [][2]string{{n.document, n.sourceVersion}, {n.target, n.targetVersion}} {
		version, err := verify(ctx, scope, pin[0])
		if errors.Is(err, lineage.ErrSourceMissing) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if version != pin[1] {
			return ErrConflict
		}
	}
	return nil
}

func (s *Service) currentRepeal(ctx context.Context, tx *sql.Tx, scope provenance.Scope, n repealNotice) error {
	for _, pin := range [][2]string{{n.document, n.sourceVersion}, {n.target, n.targetVersion}} {
		if err := s.currentEnabled(ctx, tx, scope, pin[0], pin[1]); err != nil {
			return err
		}
	}
	return nil
}

func updateRepeal(ctx context.Context, tx *sql.Tx, scope provenance.Scope, n repealNotice,
	status string, event, appliedAt any) (Result, error) {
	_, err := tx.ExecContext(ctx, `UPDATE ext_repeal_notices SET status=?,revision=revision+1,applied_event_id=?,applied_at=?
		WHERE tenant_id=? AND knowledge_base_id=? AND notice_id=?`, append([]any{status, event, appliedAt}, args(scope, n.id)...)...)
	return Result{ID: n.id, Status: status, Revision: n.revision + 1}, err
}

func applyRepeal(ctx context.Context, tx *sql.Tx, scope provenance.Scope, n repealNotice, now time.Time) (Result, error) {
	eventID := "repeal-" + n.id
	_, err := lifecycle.AppendTx(ctx, tx, lifecycle.Event{EventID: eventID, TenantID: scope.TenantID,
		KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: n.target, FromState: lifecycle.Active,
		ToState: "archived", Reason: "repeal_effective", OccurredAt: now, RequestID: n.id})
	if err != nil {
		return Result{}, err
	}
	return updateRepeal(ctx, tx, scope, n, "applied", eventID, now.Format(time.RFC3339Nano))
}

// SweepRepeals executes due approvals in configured scopes and retries unknown observations on later sweeps.
func (s *Service) SweepRepeals(ctx context.Context, scopes []provenance.Scope) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	cutoff := s.now().UnixMilli()
	var failures []error
	for _, scope := range scopes {
		cursor := repealNotice{}
		for {
			var page []repealNotice
			err := s.db.View(ctx, func(tx *sql.Tx) error {
				rows, err := tx.QueryContext(ctx, `SELECT notice_id,document_id,target_document_id,source_version_id,target_version_id,
					effective_at_ms,revision FROM ext_repeal_notices
					WHERE tenant_id=? AND knowledge_base_id=? AND status='scheduled' AND effective_at_ms<=?
					AND (effective_at_ms,notice_id)>(?,?) ORDER BY effective_at_ms,notice_id LIMIT 64`,
					args(scope, cutoff, cursor.due, cursor.id)...)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var n repealNotice
					if err := rows.Scan(&n.id, &n.document, &n.target, &n.sourceVersion, &n.targetVersion, &n.due, &n.revision); err != nil {
						return err
					}
					page = append(page, n)
				}
				return rows.Err()
			})
			if err != nil {
				return errors.Join(append(failures, err)...)
			}
			for _, n := range page {
				cursor = n
				if err := s.sweepRepeal(ctx, scope, n); err != nil {
					failures = append(failures, err)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if len(page) < 64 {
				break
			}
		}
	}
	return errors.Join(failures...)
}

func (s *Service) sweepRepeal(ctx context.Context, scope provenance.Scope, n repealNotice) error {
	observed := s.verifyRepeal(ctx, scope, n, s.observeRepealVersion)
	if observed != nil && !errors.Is(observed, ErrConflict) {
		return observed
	}
	operationID := newID()
	if operationID == "" {
		return ErrUnavailable
	}
	advanced := false
	c := Command{Scope: scope, ID: n.id, Action: "repeal_apply", ActorID: "system:repeal",
		OperationID: "repeal-apply-" + operationID, ExpectedRevision: n.revision}
	_, err := s.transact(ctx, c, func(tx *sql.Tx) (Result, error) {
		current, err := readRepeal(ctx, tx, scope, n.id)
		if err != nil {
			return Result{}, err
		}
		now := s.now().UTC()
		if current.status != "scheduled" || current.revision != n.revision || current.due > now.UnixMilli() {
			advanced = true
			return Result{}, ErrConflict
		}
		if observed == nil {
			observed = s.currentRepeal(ctx, tx, scope, current)
		}
		if errors.Is(observed, ErrConflict) {
			return updateRepeal(ctx, tx, scope, current, "needs_review", nil, nil)
		}
		if observed != nil {
			return Result{}, observed
		}
		return applyRepeal(ctx, tx, scope, current, now)
	})
	if advanced && errors.Is(err, ErrConflict) {
		return nil // A different operator or timer already advanced this revision.
	}
	return err
}

func (s *Service) observeRepealVersion(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
	version, err := s.observeVersion(ctx, scope, doc)
	if errors.Is(err, lineage.ErrEvidence) {
		if native, ok := s.remote.(*NativePublisher); ok {
			if invalid, probeErr := native.repealDocumentInactive(ctx, scope, doc); probeErr == nil && invalid {
				return "", ErrConflict
			}
		}
	}
	return version, err
}
