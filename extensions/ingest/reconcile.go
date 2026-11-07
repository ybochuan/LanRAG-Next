// Package ingest reconciles native ingest observations without changing core data.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Scope identifies one configured source tenant and knowledge base.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
}

// Snapshot contains source processing facts, not an AgentRAG governance state.
type Snapshot struct {
	Scope
	DocumentID  string
	ParseStatus string
	UpdatedAt   time.Time
}

// Page uses NextPage=0 only when the source has completed the listing.
type Page struct {
	Documents []Snapshot
	NextPage  int
}

// Source lists a configured scope through the upstream public API.
type Source interface {
	List(context.Context, Scope, int) (Page, error)
}

// Observation explicitly distinguishes reconciliation from original pipeline events.
type Observation struct {
	Snapshot
	Origin     string
	ObservedAt time.Time
}

// Observer persists observations idempotently and decides whether facts changed.
// The durable implementation must use the Extension WriteExecutor/transaction.
type Observer interface {
	Observe(context.Context, Observation) error
}

const (
	ReconcileWaiting   = "waiting"
	ReconcileFailed    = "failed"
	ReconcileRecovered = "recovered"
)

// Notice is the safe, structured report emitted by a Reconciler. Error() is
// deliberately built only from codes and counters; the original error may
// contain credentials or URLs and is never included.
type Notice struct {
	Kind        string
	Code        string
	Consecutive int
	NextRetry   time.Time
}

func (n Notice) Error() string {
	switch n.Kind {
	case ReconcileWaiting:
		return fmt.Sprintf("reconciliation_waiting_for_core code=%s consecutive=%d next_retry=%s", n.Code, n.Consecutive, n.NextRetry.UTC().Format(time.RFC3339))
	case ReconcileRecovered:
		return "reconciliation_recovered"
	default:
		return fmt.Sprintf("reconciliation_failed code=%s consecutive=%d next_retry=%s", n.Code, n.Consecutive, n.NextRetry.UTC().Format(time.RFC3339))
	}
}

var defaultReconcileBackoff = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	40 * time.Second,
	60 * time.Second,
}

// Reconciler serializes scans and reports transient failures before the next retry.
// It never infers deletion from absence or invents unobserved historical stages.
type Reconciler struct {
	Source   Source
	Sink     Observer
	Interval time.Duration
	Ready    func(context.Context) error
	Backoff  []time.Duration
	Report   func(error)
	mu       sync.Mutex
}

func (r *Reconciler) retryDelay(failures int) time.Duration {
	delays := r.Backoff
	if len(delays) == 0 {
		delays = defaultReconcileBackoff
	}
	index := failures - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	return delays[index]
}

func reconcileErrorCode(err error) string {
	var upstream *weknora.UpstreamError
	if errors.As(err, &upstream) && upstream.Code != "" {
		return upstream.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return weknora.UpstreamTimeout
	}
	return "reconciliation_internal_error"
}

func waitFor(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Sweep validates every page before forwarding observations. A sink failure may
// leave a partial application, so observers must safely accept repeated scans.
func (r *Reconciler) Sweep(ctx context.Context, scope Scope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Source == nil || r.Sink == nil || scope.TenantID == 0 || scope.KnowledgeBaseID == "" {
		return fmt.Errorf("reconciliation requires source, observer and valid scope")
	}
	var documents []Snapshot
	seen := make(map[string]bool)
	for page := 1; ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := r.Source.List(ctx, scope, page)
		if err != nil {
			return fmt.Errorf("reconcile page %d: %w", page, err)
		}
		if len(result.Documents) == 0 && result.NextPage != 0 {
			return fmt.Errorf("empty nonterminal reconciliation page %d", page)
		}
		for _, document := range result.Documents {
			if document.Scope != scope || document.DocumentID == "" || document.ParseStatus == "" || seen[document.DocumentID] {
				return fmt.Errorf("invalid or duplicate source document in reconciliation page %d", page)
			}
			seen[document.DocumentID] = true
			documents = append(documents, document)
		}
		if result.NextPage == 0 {
			break
		}
		if result.NextPage <= page {
			return fmt.Errorf("reconciliation pagination did not advance")
		}
		page = result.NextPage
	}
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Sink.Observe(ctx, Observation{Snapshot: document, Origin: "reconcile", ObservedAt: time.Now().UTC()}); err != nil {
			return fmt.Errorf("observe reconciled document %s: %w", document.DocumentID, err)
		}
	}
	return nil
}

// Run waits for Core readiness, performs an immediate scan, then periodic scans
// until cancellation. Readiness is independent of Host /health so Host can start
// before Core without emitting a false reconciliation failure.
func (r *Reconciler) Run(ctx context.Context, scopes []Scope) error {
	if r.Interval <= 0 || r.Report == nil || r.Source == nil || r.Sink == nil || len(scopes) == 0 {
		return fmt.Errorf("reconciliation requires interval, error reporter, services and scopes")
	}
	failures := 0
	lastEvent := ""
	for {
		if r.Ready != nil {
			if err := r.Ready(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				failures++
				delay := r.retryDelay(failures)
				code := reconcileErrorCode(err)
				if key := ReconcileWaiting + ":" + code; key != lastEvent {
					lastEvent = key
					r.Report(Notice{Kind: ReconcileWaiting, Code: code, Consecutive: failures, NextRetry: time.Now().UTC().Add(delay)})
				}
				if !waitFor(ctx, delay) {
					return ctx.Err()
				}
				continue
			}
		}
		var cycleErr error
		for _, scope := range scopes {
			if err := r.Sweep(ctx, scope); err != nil {
				cycleErr = err
				break
			}
		}
		if cycleErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures++
			delay := r.retryDelay(failures)
			code := reconcileErrorCode(cycleErr)
			if key := ReconcileFailed + ":" + code; key != lastEvent {
				lastEvent = key
				r.Report(Notice{Kind: ReconcileFailed, Code: code, Consecutive: failures, NextRetry: time.Now().UTC().Add(delay)})
			}
			if !waitFor(ctx, delay) {
				return ctx.Err()
			}
			continue
		}
		if failures > 0 {
			r.Report(Notice{Kind: ReconcileRecovered})
			failures = 0
			lastEvent = ""
		}
		if !waitFor(ctx, r.Interval) {
			return ctx.Err()
		}
	}
}
