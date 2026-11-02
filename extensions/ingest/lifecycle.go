package ingest

import (
	"context"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// LifecycleObserver preserves reconcile observations and retries lifecycle
// decisions even for unchanged snapshots after a crash or transaction failure.
// Observation and business transition are separate commits; success requires both.
type LifecycleObserver struct {
	observations *PersistentObserver
	machine      *lifecycle.Machine
}

// NewLifecycleObserver shares the extension DB; it never owns or closes it.
func NewLifecycleObserver(db *store.DB, now func() time.Time) *LifecycleObserver {
	return &LifecycleObserver{NewObserver(db), lifecycle.NewMachine(db, now)}
}

// Observe promotes only persisted completed observations, respecting validity.
func (o *LifecycleObserver) Observe(ctx context.Context, obs Observation) error {
	if err := o.observations.Observe(ctx, obs); err != nil {
		return err
	}
	if obs.ParseStatus != "completed" {
		return nil
	}
	return o.machine.ObserveCompleted(ctx, lifecycle.Scope{TenantID: obs.TenantID, KnowledgeBaseID: obs.KnowledgeBaseID, DocumentID: obs.DocumentID}, obs.UpdatedAt)
}
