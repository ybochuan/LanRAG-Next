package outbox

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

var testTime = time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*store.DB, *Store) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "agentrag_ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db, NewStore(db)
}

func enqueue(t *testing.T, db *store.DB, id string) {
	t.Helper()
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		return EnqueueTx(context.Background(), tx, Item{ID: id, TenantID: 7, KnowledgeBaseID: "kb-a",
			ProjectionType: "tag_projection", AggregateType: "document", AggregateID: "doc-a",
			CausationType: "lifecycle_event", CausationID: id, Payload: []byte(`{"state":"active"}`), CreatedAt: testTime})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredLeaseIsReclaimedAndOldWorkerIsFenced(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	ctx := context.Background()
	first, err := s.Claim(ctx, testTime, 10*time.Second)
	if err != nil || first == nil {
		t.Fatalf("first=%v %v", first, err)
	}
	if next, err := s.Claim(ctx, testTime.Add(time.Second), 10*time.Second); err != nil || next != nil {
		t.Fatalf("double claim=%v %v", next, err)
	}
	now := testTime.Add(11 * time.Second)
	if err := s.Complete(ctx, first.ID, first.LeaseToken, now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired completion=%v", err)
	}
	second, err := s.Claim(ctx, now, 10*time.Second)
	if err != nil || second == nil || first.LeaseToken == second.LeaseToken {
		t.Fatalf("reclaim=%v %v", second, err)
	}
	if err := s.Complete(ctx, first.ID, first.LeaseToken, now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old completion=%v", err)
	}
	if err := s.Fail(ctx, first.ID, first.LeaseToken, now, errors.New("old error"), time.Second, 3); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old failure=%v", err)
	}
	if err := s.Complete(ctx, second.ID, second.LeaseToken, now); err != nil {
		t.Fatal(err)
	}
	if item, err := s.Claim(ctx, now.Add(time.Hour), time.Minute); err != nil || item != nil {
		t.Fatalf("completed item reclaimed=%v %v", item, err)
	}
}

func TestRetryDeadVisibilityManualRetryAndBacklog(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	ctx := context.Background()
	stats, err := s.Stats(ctx, Scope{7, "kb-a"}, testTime.Add(31*time.Second))
	if err != nil || stats.Pending != 1 || stats.OldestPendingAge != 31*time.Second {
		t.Fatalf("stats=%+v %v", stats, err)
	}
	item, err := s.Claim(ctx, testTime, time.Minute)
	if err != nil || item == nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, item.ID, item.LeaseToken, testTime, errors.New("remote unavailable"), 2*time.Second, 2); err != nil {
		t.Fatal(err)
	}
	if item, err := s.Claim(ctx, testTime.Add(time.Second), time.Minute); err != nil || item != nil {
		t.Fatalf("retry too early=%v %v", item, err)
	}
	item, err = s.Claim(ctx, testTime.Add(2*time.Second), time.Minute)
	if err != nil || item == nil || item.RetryCount != 1 {
		t.Fatalf("retry=%v %v", item, err)
	}
	if err := s.Fail(ctx, item.ID, item.LeaseToken, testTime.Add(2*time.Second), errors.New("still unavailable"), time.Second, 2); err != nil {
		t.Fatal(err)
	}
	dead, err := s.ListDead(ctx, Scope{7, "kb-a"}, 10)
	if err != nil || len(dead) != 1 || dead[0].RetryCount != 2 || dead[0].LastError == "" {
		t.Fatalf("dead=%+v %v", dead, err)
	}
	if foreign, err := s.ListDead(ctx, Scope{8, "kb-a"}, 10); err != nil || len(foreign) != 0 {
		t.Fatalf("scope leak=%v %v", foreign, err)
	}
	stats, err = s.Stats(ctx, Scope{7, "kb-a"}, testTime.Add(3*time.Second))
	if err != nil || stats.Dead != 1 || stats.Pending != 0 || stats.Processing != 0 {
		t.Fatalf("dead stats=%+v %v", stats, err)
	}
	if err := s.RetryDead(ctx, Scope{8, "kb-a"}, "one", testTime); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-tenant retry=%v", err)
	}
	if err := s.RetryDead(ctx, Scope{7, "kb-a"}, "one", testTime.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	item, err = s.Claim(ctx, testTime.Add(3*time.Second), time.Minute)
	if err != nil || item == nil || item.RetryCount != 0 {
		t.Fatalf("manual retry=%v %v", item, err)
	}
}

func TestConcurrentClaimersCannotOwnTheSameLiveItem(t *testing.T) {
	db, s := fixture(t)
	enqueue(t, db, "one")
	var workers sync.WaitGroup
	results := make(chan *Item, 100)
	errors := make(chan error, 100)
	for i := 0; i < 100; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			item, err := s.Claim(context.Background(), testTime, time.Minute)
			results <- item
			errors <- err
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	claims := 0
	for item := range results {
		if item != nil {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("live owners=%d", claims)
	}
}

func TestScopedClaimLeavesForeignPendingAndExpiredLeasesUntouched(t *testing.T) {
	db, s := fixture(t)
	ctx := context.Background()
	for _, entry := range []struct {
		id    string
		scope Scope
	}{{"foreign", Scope{8, "kb-a"}}, {"other-kb", Scope{7, "kb-b"}}, {"allowed", Scope{7, "kb-a"}}} {
		if err := db.Write(ctx, func(tx *sql.Tx) error {
			return EnqueueTx(ctx, tx, Item{ID: entry.id, TenantID: entry.scope.TenantID, KnowledgeBaseID: entry.scope.KnowledgeBaseID, ProjectionType: "tag_projection", AggregateType: "document", AggregateID: entry.id, CausationType: "lifecycle_event", CausationID: entry.id, Payload: []byte(`{}`), CreatedAt: testTime})
		}); err != nil {
			t.Fatal(err)
		}
	}
	foreign, err := s.ClaimScoped(ctx, testTime, time.Second, []Scope{{8, "kb-a"}})
	if err != nil || foreign == nil {
		t.Fatalf("foreign setup=%v %v", foreign, err)
	}
	item, err := s.ClaimScoped(ctx, testTime.Add(2*time.Second), time.Minute, []Scope{{7, "kb-a"}})
	if err != nil || item == nil || item.ID != "allowed" {
		t.Fatalf("scoped claim=%v %v", item, err)
	}
	if item, err := s.ClaimScoped(ctx, testTime.Add(2*time.Second), time.Minute, []Scope{{7, "kb-a"}}); err != nil || item != nil {
		t.Fatalf("foreign claim=%v %v", item, err)
	}
	for _, scopes := range [][]Scope{nil, {}, {{0, "kb-a"}}, {{7, ""}}} {
		if _, err := s.ClaimScoped(ctx, testTime, time.Minute, scopes); err == nil {
			t.Fatal("empty/invalid scope widened claim")
		}
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var status, token string
		if err := tx.QueryRow("SELECT status,lease_token FROM ext_projection_outbox WHERE outbox_id='foreign'").Scan(&status, &token); err != nil {
			return err
		}
		if status != "processing" || token != foreign.LeaseToken {
			t.Error("foreign expired lease was changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
