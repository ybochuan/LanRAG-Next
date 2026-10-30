package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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
