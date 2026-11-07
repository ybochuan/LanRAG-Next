package ingest

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

type sourceFunc func(context.Context, Scope, int) (Page, error)

func (f sourceFunc) List(ctx context.Context, scope Scope, page int) (Page, error) {
	return f(ctx, scope, page)
}

type observerFunc func(context.Context, Observation) error

func (f observerFunc) Observe(ctx context.Context, observation Observation) error {
	return f(ctx, observation)
}

func TestReconcileReadsAllPagesBeforeObserving(t *testing.T) {
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	pagesRead := 0
	var observed []Observation
	reconciler := Reconciler{
		Source: sourceFunc(func(_ context.Context, got Scope, page int) (Page, error) {
			if got != scope {
				t.Fatalf("wrong source scope: %+v", got)
			}
			pagesRead++
			if page == 1 {
				return Page{Documents: []Snapshot{{Scope: scope, DocumentID: "a", ParseStatus: "processing"}}, NextPage: 2}, nil
			}
			return Page{Documents: []Snapshot{{Scope: scope, DocumentID: "b", ParseStatus: "completed"}}}, nil
		}),
		Sink: observerFunc(func(_ context.Context, observation Observation) error {
			if pagesRead != 2 {
				t.Fatal("partial listing must not be applied")
			}
			observed = append(observed, observation)
			return nil
		}),
	}
	if err := reconciler.Sweep(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0].DocumentID != "a" || observed[1].DocumentID != "b" {
		t.Fatalf("unexpected observations: %+v", observed)
	}
	for _, observation := range observed {
		if observation.Origin != "reconcile" || observation.ObservedAt.IsZero() {
			t.Fatalf("must identify reconciled observation: %+v", observation)
		}
	}
}

func TestReconcileRejectsIncompleteOrInvalidSnapshots(t *testing.T) {
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	for _, failure := range []string{"page_failure", "repeated_page", "wrong_scope", "duplicate_document", "empty_nonterminal"} {
		t.Run(failure, func(t *testing.T) {
			writes := 0
			reconciler := Reconciler{
				Source: sourceFunc(func(_ context.Context, _ Scope, page int) (Page, error) {
					if page == 2 {
						if failure == "page_failure" {
							return Page{}, errors.New("source unavailable")
						}
						return Page{Documents: []Snapshot{{Scope: scope, DocumentID: "a", ParseStatus: "completed"}}}, nil
					}
					doc := Snapshot{Scope: scope, DocumentID: "a", ParseStatus: "processing"}
					next := 2
					if failure == "repeated_page" {
						next = 1
					}
					if failure == "wrong_scope" {
						doc.TenantID = 99
					}
					if failure == "empty_nonterminal" {
						return Page{NextPage: 2}, nil
					}
					return Page{Documents: []Snapshot{doc}, NextPage: next}, nil
				}),
				Sink: observerFunc(func(context.Context, Observation) error { writes++; return nil }),
			}
			if err := reconciler.Sweep(context.Background(), scope); err == nil || writes != 0 {
				t.Fatalf("invalid scan must fail before writes, err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestReconcileBackoffSchedule(t *testing.T) {
	reconciler := Reconciler{}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	for attempt, expected := range want {
		if got := reconciler.retryDelay(attempt + 1); got != expected {
			t.Fatalf("attempt %d delay = %v, want %v", attempt+1, got, expected)
		}
	}
	custom := Reconciler{Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond}}
	if got := custom.retryDelay(3); got != 2*time.Millisecond {
		t.Fatalf("custom capped delay = %v", got)
	}
}

func TestReconcileWaitsForCoreReadinessBeforeFirstSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	var readyCalls int32
	var sweeps int32
	observed := make(chan struct{}, 1)
	reports := make(chan string, 16)
	reconciler := Reconciler{
		Interval: time.Millisecond,
		Backoff:  []time.Duration{time.Millisecond},
		Ready: func(context.Context) error {
			if atomic.AddInt32(&readyCalls, 1) < 3 {
				return &weknora.UpstreamError{Code: weknora.UpstreamUnreachable}
			}
			return nil
		},
		Source: sourceFunc(func(context.Context, Scope, int) (Page, error) {
			atomic.AddInt32(&sweeps, 1)
			return Page{Documents: []Snapshot{{Scope: scope, DocumentID: "a", ParseStatus: "completed"}}}, nil
		}),
		Sink: observerFunc(func(context.Context, Observation) error {
			select {
			case observed <- struct{}{}:
			default:
			}
			return nil
		}),
		Report: func(err error) { reports <- err.Error() },
	}
	done := make(chan error, 1)
	go func() { done <- reconciler.Run(ctx, []Scope{scope}) }()
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("source was not scanned after readiness")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run exit = %v", err)
	}
	if atomic.LoadInt32(&sweeps) == 0 {
		t.Fatal("source scanned before readiness")
	}
	close(reports)
	var text []string
	for report := range reports {
		text = append(text, report)
	}
	joined := strings.Join(text, "\n")
	if !strings.Contains(joined, "reconciliation_waiting_for_core") || !strings.Contains(joined, "reconciliation_recovered") {
		t.Fatalf("missing readiness lifecycle reports: %s", joined)
	}
}

func TestReconcileContextCancellationDuringReadinessBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reports := make(chan string, 1)
	reconciler := Reconciler{
		Interval: time.Second,
		Backoff:  []time.Duration{time.Hour},
		Ready: func(context.Context) error {
			return &weknora.UpstreamError{Code: weknora.UpstreamUnreachable}
		},
		Source: sourceFunc(func(context.Context, Scope, int) (Page, error) {
			return Page{}, errors.New("must not be called")
		}),
		Sink:   observerFunc(func(context.Context, Observation) error { return nil }),
		Report: func(err error) { reports <- err.Error() },
	}
	done := make(chan error, 1)
	go func() { done <- reconciler.Run(ctx, []Scope{{TenantID: 7, KnowledgeBaseID: "kb-a"}}) }()
	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("readiness failure was not reported")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run exit = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run ignored context cancellation")
	}
}

func TestReconcileFailureReportIsClassifiedAndRedacted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reports := make(chan string, 1)
	reconciler := Reconciler{
		Interval: time.Second,
		Backoff:  []time.Duration{time.Millisecond},
		Source: sourceFunc(func(context.Context, Scope, int) (Page, error) {
			return Page{}, errors.New("secret token kb-a")
		}),
		Sink:   observerFunc(func(context.Context, Observation) error { return nil }),
		Report: func(err error) { reports <- err.Error() },
	}
	done := make(chan error, 1)
	go func() { done <- reconciler.Run(ctx, []Scope{{TenantID: 7, KnowledgeBaseID: "kb-a"}}) }()
	var report string
	select {
	case report = <-reports:
	case <-time.After(time.Second):
		t.Fatal("failure was not reported")
	}
	cancel()
	<-done
	if !strings.Contains(report, "reconciliation_failed code=reconciliation_internal_error") {
		t.Fatalf("failure not classified: %s", report)
	}
	if strings.Contains(report, "secret") || strings.Contains(report, "kb-a") {
		t.Fatalf("failure report leaked source data: %s", report)
	}
}

func TestReconcileRepeatedSweepDoesNotDuplicateObservationsOrEvents(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "agentrag_ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	snapshot := Snapshot{Scope: scope, DocumentID: "doc-a", ParseStatus: "completed", UpdatedAt: time.Date(2026, 10, 30, 0, 0, 0, 123, time.UTC)}
	reconciler := Reconciler{
		Source: sourceFunc(func(context.Context, Scope, int) (Page, error) {
			return Page{Documents: []Snapshot{snapshot}}, nil
		}),
		Sink: NewLifecycleObserver(db, time.Now),
	}
	counts := func() [4]int {
		var got [4]int
		if err := db.View(ctx, func(tx *sql.Tx) error {
			for index, table := range []string{"ext_ingest_observations", "ext_ingest_current", "ext_lifecycle_events", "ext_projection_outbox"} {
				if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got[index]); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if err := reconciler.Sweep(ctx, scope); err != nil {
		t.Fatal(err)
	}
	first := counts()
	if err := reconciler.Sweep(ctx, scope); err != nil {
		t.Fatal(err)
	}
	second := counts()
	if first != second {
		t.Fatalf("repeated sweep changed durable facts: first=%v second=%v", first, second)
	}
	if first[0] != 1 || first[1] != 1 {
		t.Fatalf("unexpected observation counts: %v", first)
	}
}

func TestReconcileReportsFailureAndRetriesOnNextTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}
	var mu sync.Mutex
	attempts := 0
	reported := make(chan error, 1)
	recovered := make(chan struct{}, 1)
	reconciler := Reconciler{
		Interval: 5 * time.Millisecond,
		Backoff:  []time.Duration{time.Millisecond},
		Source: sourceFunc(func(context.Context, Scope, int) (Page, error) {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			if attempts == 1 {
				return Page{}, errors.New("temporary failure")
			}
			return Page{Documents: []Snapshot{{Scope: scope, DocumentID: "a", ParseStatus: "completed"}}}, nil
		}),
		Sink: observerFunc(func(context.Context, Observation) error {
			select {
			case recovered <- struct{}{}:
			default:
			}
			return nil
		}),
		Report: func(err error) {
			select {
			case reported <- err:
			default:
			}
		},
	}
	done := make(chan error, 1)
	go func() { done <- reconciler.Run(ctx, []Scope{scope}) }()
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("missing source error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation did not report failure")
	}
	select {
	case <-recovered:
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation did not recover on next tick")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run exit = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation did not stop")
	}
}
