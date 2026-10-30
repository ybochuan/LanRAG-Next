// Package ingest reconciles native ingest observations without changing core data.
package ingest

import (
	"context"
	"fmt"
	"sync"
	"time"
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

// Reconciler serializes scans and reports transient failures before the next retry.
// It never infers deletion from absence or invents unobserved historical stages.
type Reconciler struct {
	Source   Source
	Sink     Observer
	Interval time.Duration
	Report   func(error)
	mu       sync.Mutex
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

// Run performs an immediate scan, then periodic scans until cancellation.
// Report is required so failures cannot silently disappear into an in-memory loop.
func (r *Reconciler) Run(ctx context.Context, scopes []Scope) error {
	if r.Interval <= 0 || r.Report == nil || r.Source == nil || r.Sink == nil || len(scopes) == 0 {
		return fmt.Errorf("reconciliation requires interval, error reporter, services and scopes")
	}
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		for _, scope := range scopes {
			if err := r.Sweep(ctx, scope); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				r.Report(err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
