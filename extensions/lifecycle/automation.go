package lifecycle

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ObserveCompleted advances a persisted reconcile observation. Promotion checks
// the latest completed source observation again inside the decision transaction.
// Its stage is an observation, not an invented native pipeline completion event.
func (m *Machine) ObserveCompleted(ctx context.Context, s Scope, sourceUpdatedAt time.Time) error {
	if sourceUpdatedAt.IsZero() {
		return errors.New("completed observation timestamp required")
	}
	return m.advance(ctx, s, ParseCompleted, &sourceUpdatedAt)
}

// Reevaluate applies the existing Timer rules to one explicitly scoped document.
// It never reactivates invalidated documents or bypasses completed-observation guards.
func (m *Machine) Reevaluate(ctx context.Context, s Scope) error {
	if err := s.validate(); err != nil {
		return err
	}
	return m.advance(ctx, s, Timer, nil)
}

func (m *Machine) advance(ctx context.Context, s Scope, trigger Trigger, source *time.Time) error {
	for attempt := 0; attempt < 8; attempt++ {
		snap, err := m.Snapshot(ctx, s)
		if err != nil {
			return err
		}
		now := m.now().UTC()
		from := snap.Current.State
		var to State
		action := trigger
		switch {
		case from == "" && trigger == ParseCompleted:
			to = Pending
			action = Initialize
		case (from == Pending || from == Scheduled || from == Active) && snap.Dependencies.Configured && snap.Dependencies.Invalid(now):
			to = Stale
			action = DependencyInvalidated
		case (from == Pending || from == Scheduled || from == Active) && overdue(now, snap.Annotations.EffectiveUntil):
			to = Stale
			if from != Pending {
				action = Timer
			}
		case from == Pending && trigger == ParseCompleted:
			to = Active
			if snap.Annotations.EffectiveFrom != nil && now.Before(*snap.Annotations.EffectiveFrom) {
				to = Scheduled
			}
		case from == Scheduled && trigger == Timer && snap.Annotations.EffectiveFrom != nil && !now.Before(*snap.Annotations.EffectiveFrom):
			to = Active
		default:
			return nil
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		c := Command{Event: Event{EventID: hex.EncodeToString(nonce[:]), TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: s.DocumentID, FromState: from, ToState: to, Reason: "lifecycle_" + string(action), OccurredAt: now}, Trigger: action, ExpectedLastEventID: snap.Current.LastEventID, ExpectedAnnotationRevision: snap.Annotations.Revision, ExpectedPolicyToken: snap.Policy.Token}
		if source != nil {
			c.Event.SourceEventID = "reconcile:" + source.UTC().Format(time.RFC3339Nano)
			c.Event.Stage = "reconcile_" + string(action)
			if action == ParseCompleted {
				c.ParseObservationAt = source
			}
		}
		if _, err = m.Apply(ctx, c); err != nil {
			if errors.Is(err, ErrStateConflict) || errors.Is(err, ErrConfigurationConflict) || errors.Is(err, ErrEventConflict) {
				// A newer observation must not be retried as the old completed snapshot.
				if errors.Is(err, ErrStateConflict) && source != nil && action == ParseCompleted {
					return err
				}
				continue
			}
			return err
		}
	}
	return errors.New("lifecycle automation contention; retry next reconciliation")
}

// Sweep processes bounded pages of current facts in explicit tenant/KB scopes.
// It compares parsed time.Time values, not variable-width RFC3339 text ordering.
func (m *Machine) Sweep(ctx context.Context, scopes []PolicyScope) error {
	if len(scopes) == 0 || len(scopes) > 1000 {
		return errors.New("lifecycle scan scopes required")
	}
	for _, scope := range scopes {
		if scope.TenantID == 0 || scope.KnowledgeBaseID == "" {
			return errors.New("explicit scan tenant/KB required")
		}
	}
	var failures []error
	for _, scope := range scopes {
		cursor := ""
		for {
			var ids []string
			err := m.db.View(ctx, func(tx *sql.Tx) error {
				rows, err := tx.QueryContext(ctx, `SELECT document_id FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id>? AND state IN ('pending','scheduled','active') ORDER BY document_id LIMIT 256`, fmt.Sprint(scope.TenantID), scope.KnowledgeBaseID, cursor)
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
				if err = m.advance(ctx, Scope{scope.TenantID, scope.KnowledgeBaseID, id}, Timer, nil); err != nil {
					failures = append(failures, err)
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

// RunDaily performs a recovery sweep immediately, then every 24 hours. Failures
// are visible and retried next sweep; canceled tasks are joined by Host.
func (m *Machine) RunDaily(ctx context.Context, scopes []PolicyScope, report func(error)) error {
	if report == nil {
		return errors.New("lifecycle scan reporter required")
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if err := m.Sweep(ctx, scopes); err != nil {
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
