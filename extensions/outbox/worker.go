package outbox

import (
	"context"
	"errors"
	"time"
)

// Worker delivers projections at least once. Handler must honor cancellation,
// be idempotent, and reconcile current truth before changing a remote projection.
// A handler cannot use a database lease to fence remote HTTP side effects.
type Worker struct {
	Store        *Store
	Handler      func(context.Context, Item) error
	Lease        time.Duration
	RetryDelay   time.Duration
	MaxAttempts  int
	PollInterval time.Duration
	Now          func() time.Time
	Report       func(error)
	// Scopes nil preserves the standalone global worker; Hosts must set a
	// nonempty allowlist. An explicit empty slice is rejected by ClaimScoped.
	Scopes []Scope
}

func (w *Worker) validate() error {
	if w.Store == nil || w.Handler == nil || w.Report == nil || w.Lease < time.Millisecond || w.RetryDelay < time.Millisecond || w.MaxAttempts < 1 || w.PollInterval < time.Millisecond {
		return errors.New("complete outbox worker configuration and error reporter required")
	}
	if w.Scopes != nil {
		if len(w.Scopes) == 0 || len(w.Scopes) > 1000 {
			return errors.New("worker requires 1..1000 explicit scopes")
		}
		seen := make(map[Scope]bool)
		for _, scope := range w.Scopes {
			if err := scope.validate(); err != nil {
				return err
			}
			if seen[scope] {
				return errors.New("duplicate worker scope")
			}
			seen[scope] = true
		}
	}
	return nil
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// ProcessOne returns worked=false only if no claim was made. A failed projection
// is reported and persisted for retry; cancellation leaves a recoverable lease.
func (w *Worker) ProcessOne(ctx context.Context) (worked bool, err error) {
	defer func() {
		if err != nil && w.Report != nil {
			w.Report(err)
		}
	}()
	if err := w.validate(); err != nil {
		return false, err
	}
	var item *Item
	if w.Scopes == nil {
		item, err = w.Store.Claim(ctx, w.now(), w.Lease)
	} else {
		item, err = w.Store.ClaimScoped(ctx, w.now(), w.Lease, w.Scopes)
	}
	if err != nil || item == nil {
		return false, err
	}
	handlerCtx, cancel := context.WithTimeout(ctx, w.Lease)
	failure := w.Handler(handlerCtx, *item)
	if failure == nil {
		failure = handlerCtx.Err()
	}
	cancel()
	if failure != nil {
		persistErr := w.Store.Fail(ctx, item.ID, item.LeaseToken, w.now(), failure, w.RetryDelay, w.MaxAttempts)
		return true, errors.Join(failure, persistErr)
	}
	return true, w.Store.Complete(ctx, item.ID, item.LeaseToken, w.now())
}

// Run drains healthy work immediately and polls after idle/error outcomes.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := w.ProcessOne(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
