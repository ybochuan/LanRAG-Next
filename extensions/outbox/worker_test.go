package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPersistsFailureThenDrainsHealthyBacklog(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	ctx := context.Background()
	now := testTime
	failing := true
	reports := 0
	w := Worker{Store: s, Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 2, PollInterval: time.Second,
		Now: func() time.Time { return now }, Report: func(error) { reports++ },
		Handler: func(context.Context, Item) error {
			if failing {
				return errors.New("projection failed")
			}
			return nil
		}}
	if worked, err := w.ProcessOne(ctx); !worked || err == nil {
		t.Fatalf("failure=%v %v", worked, err)
	}
	failing = false
	now = now.Add(2 * time.Second)
	if worked, err := w.ProcessOne(ctx); !worked || err != nil {
		t.Fatalf("retry=%v %v", worked, err)
	}
	stats, err := s.Stats(ctx, Scope{7, "kb-a"}, now)
	if err != nil || stats.Pending != 0 || stats.Processing != 0 || stats.Dead != 0 || stats.OldestPendingAge != 0 {
		t.Fatalf("healthy=%+v %v", stats, err)
	}
	if reports != 1 {
		t.Fatalf("failure report count=%d", reports)
	}
}

func TestWorkerCancellationLeavesClaimRecoverableAndRunStops(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	ctx, cancel := context.WithCancel(context.Background())
	w := Worker{Store: s, Lease: time.Second, RetryDelay: time.Second, MaxAttempts: 3, PollInterval: time.Second,
		Now: func() time.Time { return testTime }, Report: func(error) {},
		Handler: func(ctx context.Context, _ Item) error { cancel(); <-ctx.Done(); return ctx.Err() }}
	if _, err := w.ProcessOne(ctx); err == nil {
		t.Fatal("canceled projection acknowledged")
	}
	if item, err := s.Claim(context.Background(), testTime.Add(2*time.Second), time.Minute); err != nil || item == nil {
		t.Fatalf("canceled claim not recoverable=%v %v", item, err)
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run cancel=%v", err)
	}
}

func TestRunRetriesReportsAndDrainsHealthyBacklog(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	enqueue(t, db, "two")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var calls atomic.Int32
	reports := make(chan error, 10)
	w := Worker{Store: s, Lease: time.Minute, RetryDelay: 10 * time.Millisecond, MaxAttempts: 3, PollInterval: 5 * time.Millisecond,
		Report: func(err error) { reports <- err }, Handler: func(context.Context, Item) error {
			if calls.Add(1) == 1 {
				return errors.New("one transient failure")
			}
			return nil
		}}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		stats, err := s.Stats(ctx, Scope{7, "kb-a"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if stats.Pending == 0 && stats.Processing == 0 {
			if stats.Dead != 0 || stats.OldestPendingAge >= 30*time.Second {
				t.Fatalf("unhealthy after drain: %+v", stats)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("healthy worker did not drain backlog")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run exit=%v", err)
	}
	if calls.Load() != 3 || len(reports) != 1 {
		t.Fatalf("calls=%d reports=%d", calls.Load(), len(reports))
	}
}

func TestHandlerDeadlineCountsFailureAndReachesDead(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "timeout")
	w := Worker{Store: s, Lease: 20 * time.Millisecond, RetryDelay: time.Millisecond, MaxAttempts: 1, PollInterval: time.Millisecond,
		Report: func(error) {}, Handler: func(ctx context.Context, _ Item) error { <-ctx.Done(); return ctx.Err() }}
	worked, err := w.ProcessOne(context.Background())
	if !worked || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v %v", worked, err)
	}
	dead, err := s.ListDead(context.Background(), Scope{7, "kb-a"}, 10)
	if err != nil || len(dead) != 1 || dead[0].RetryCount != 1 || dead[0].LastError == "" {
		t.Fatalf("timeout failure not persisted: dead=%+v err=%v", dead, err)
	}
}

func TestWorkerHonorsScopeAndRejectsExplicitEmptyAllowlist(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "scoped")
	calls := 0
	w := Worker{Store: s, Handler: func(context.Context, Item) error { calls++; return nil }, Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 3, PollInterval: time.Second, Now: func() time.Time { return testTime }, Report: func(error) {}, Scopes: []Scope{{7, "other-kb"}}}
	if worked, err := w.ProcessOne(context.Background()); err != nil || worked || calls != 0 {
		t.Fatalf("foreign work=%v %v calls=%d", worked, err, calls)
	}
	w.Scopes = []Scope{{7, "kb-a"}}
	if worked, err := w.ProcessOne(context.Background()); err != nil || !worked || calls != 1 {
		t.Fatalf("allowed work=%v %v calls=%d", worked, err, calls)
	}
	w.Scopes = []Scope{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Run(ctx); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("explicit empty scopes not rejected at validation: %v", err)
	}
}
