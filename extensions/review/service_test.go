package review

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func fixture(t *testing.T) (*store.DB, *Service, *time.Time, lifecycle.Scope) {
	t.Helper()
	db, err := store.OpenDurable(context.Background(), filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	s := NewService(db, func() time.Time { return now })
	return db, s, &now, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
}

func TestSweepPaginatesAndNeverTouchesUnconfiguredScopes(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	due := now.Add(-time.Hour).Format(time.RFC3339Nano)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for i := 0; i < 257; i++ {
			if _, err := tx.Exec(`INSERT INTO ext_document_annotations VALUES('7','kb-a',?,1,NULL,NULL,?,'{}')`, fmt.Sprintf("doc-%03d", i), due); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO ext_document_annotations VALUES('8','kb-a','foreign',1,NULL,NULL,?,'{}'),('7','kb-b','foreign-kb',1,NULL,NULL,?,'{}')`, due, due)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb-a"}}); err != nil {
		t.Fatal(err)
	}
	if a, q := counts(t, db); a != 257 || q != 257 {
		t.Fatalf("alerts=%d intents=%d", a, q)
	}
	if alerts, err := s.List(ctx, outbox.Scope{TenantID: 8, KnowledgeBaseID: scope.KnowledgeBaseID}, 100); err != nil || len(alerts) != 0 {
		t.Fatalf("foreign alerts=%v %v", alerts, err)
	}
}

func TestReviewLeavesExistingLifecycleHeadUntouched(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	original := lifecycle.Event{EventID: "active-head", TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, DocumentID: scope.DocumentID, ToState: lifecycle.Active, Reason: "seed", OccurredAt: *now}
	if _, err := lifecycle.NewStore(db).Append(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.NewStore(db).Current(ctx, scope)
	if err != nil || current.State != lifecycle.Active || current.LastEventID != original.EventID {
		t.Fatalf("%+v %v", current, err)
	}
}

func TestDeadReviewCanBeRetriedWithoutChangingTruth(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	q := outbox.NewStore(db)
	failure := errors.New("local notification failed")
	w := outbox.Worker{Store: q, Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 1, PollInterval: time.Millisecond, Now: func() time.Time { return *now }, Report: func(error) {}, Handler: s.Handler(func(context.Context, Alert) error { return failure })}
	if _, err := w.ProcessOne(ctx); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 || alerts[0].DeliveryState != "dead" || alerts[0].DeliveredAt != nil {
		t.Fatal(alerts)
	}
	scoped := outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}
	dead, err := q.ListDead(ctx, scoped, 10)
	if err != nil || len(dead) != 1 {
		t.Fatalf("%v %v", dead, err)
	}
	if err = q.RetryDead(ctx, outbox.Scope{TenantID: 8, KnowledgeBaseID: scope.KnowledgeBaseID}, dead[0].ID, *now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if err = q.RetryDead(ctx, scoped, dead[0].ID, *now); err != nil {
		t.Fatal(err)
	}
	w.Handler = s.Handler(func(context.Context, Alert) error { return nil })
	if _, err = w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if alerts = list(t, s, scope); len(alerts) != 1 || alerts[0].Generation != 1 || alerts[0].DeliveredAt == nil || alerts[0].DeliveryState != "done" {
		t.Fatal(alerts)
	}
}

func TestConcurrentDuplicateDeliveriesSignalOnceAfterCommit(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	item, err := outbox.NewStore(db).Claim(ctx, *now, time.Minute)
	if err != nil || item == nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := s.Handler(func(context.Context, Alert) error { calls.Add(1); return nil })
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := handler(ctx, *item); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	bad := *item
	bad.TenantID = 8
	if err = handler(ctx, bad); err == nil {
		t.Fatal("cross-tenant alert identity accepted")
	}
}
func annotate(t *testing.T, db *store.DB, scope lifecycle.Scope, due *time.Time, revision int64) {
	t.Helper()
	if err := lifecycle.NewConfiguration(db).PutAnnotations(context.Background(), scope, lifecycle.Annotations{ReviewDue: due, Labels: map[string]string{"kind": "notice"}}, revision); err != nil {
		t.Fatal(err)
	}
}
func list(t *testing.T, s *Service, scope lifecycle.Scope) []Alert {
	t.Helper()
	alerts, err := s.List(context.Background(), outbox.Scope{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID}, 100)
	if err != nil {
		t.Fatal(err)
	}
	return alerts
}
func counts(t *testing.T, db *store.DB) (alerts, intents int) {
	t.Helper()
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_review_alerts").Scan(&alerts); err != nil {
			return err
		}
		return tx.QueryRow("SELECT COUNT(*) FROM ext_projection_outbox WHERE projection_type='review_alert'").Scan(&intents)
	}); err != nil {
		t.Fatal(err)
	}
	return
}

func TestReviewDeadlineDeduplicatesWithoutLifecycleMutation(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	due := *now
	annotate(t, db, scope, &due, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if len(list(t, s, scope)) != 0 {
		t.Fatal("equal boundary alerted")
	}
	*now = now.Add(time.Nanosecond)
	for i := 0; i < 3; i++ {
		if err := s.Check(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	annotate(t, db, scope, &due, 1)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if a, q := counts(t, db); a != 1 || q != 1 {
		t.Fatalf("alerts=%d intents=%d", a, q)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 || alerts[0].Generation != 1 || alerts[0].DocumentID != scope.DocumentID {
		t.Fatal(alerts)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"ext_lifecycle_events", "ext_lifecycle_current"} {
			var n int
			if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Fatal("review invented lifecycle fact")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDateChangeSuppressesOldDeliveryAndReopensOneGeneration(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	item, err := outbox.NewStore(db).Claim(ctx, *now, time.Minute)
	if err != nil || item == nil {
		t.Fatalf("%+v %v", item, err)
	}
	future := now.Add(time.Hour)
	annotate(t, db, scope, &future, 1)
	if len(list(t, s, scope)) != 0 {
		t.Fatal("obsolete deadline visible")
	}
	sent := 0
	handler := s.Handler(func(context.Context, Alert) error { sent++; return nil })
	if err = handler(ctx, *item); err != nil || sent != 0 {
		t.Fatalf("obsolete signal: %d %v", sent, err)
	}
	annotate(t, db, scope, &past, 2)
	if err = s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 || alerts[0].Generation != 2 {
		t.Fatal(alerts)
	}
	if err = handler(ctx, *item); err != nil || sent != 0 {
		t.Fatalf("old generation sent: %d %v", sent, err)
	}
	if a, q := counts(t, db); a != 1 || q != 2 {
		t.Fatalf("alerts=%d intents=%d", a, q)
	}
}

func TestConcurrentReviewChecksHaveOneIntent(t *testing.T) {
	db, s, now, scope := fixture(t)
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Check(context.Background(), scope); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if a, q := counts(t, db); a != 1 || q != 1 {
		t.Fatalf("alerts=%d intents=%d", a, q)
	}
}

func TestReviewOutboxFailureRollsBackInbox(t *testing.T) {
	db, s, now, scope := fixture(t)
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TRIGGER fail_review_outbox BEFORE INSERT ON ext_projection_outbox BEGIN SELECT RAISE(ABORT,'injected'); END")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background(), scope); err == nil {
		t.Fatal("injection did not fail")
	}
	if a, q := counts(t, db); a != 0 || q != 0 {
		t.Fatalf("partial alerts=%d intents=%d", a, q)
	}
}

func TestReviewDeliveryRetriesAndNeverDuplicatesCommittedSignal(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	failure := errors.New("local notification unavailable")
	w := outbox.Worker{Store: outbox.NewStore(db), Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 2, PollInterval: time.Millisecond, Now: func() time.Time { return *now }, Report: func(error) {}, Handler: s.Handler(func(context.Context, Alert) error {
		if calls.Add(1) == 1 {
			return failure
		}
		return nil
	})}
	if _, err := w.ProcessOne(ctx); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if list(t, s, scope)[0].DeliveredAt != nil {
		t.Fatal("failed signal marked delivered")
	}
	*now = now.Add(2 * time.Second)
	if _, err := w.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if alerts[0].DeliveredAt == nil || calls.Load() != 2 {
		t.Fatal(alerts)
	}
	var item outbox.Item
	var payload string
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT outbox_id,tenant_id,knowledge_base_id,projection_type,aggregate_type,aggregate_id,causation_type,causation_id,payload_json FROM ext_projection_outbox WHERE projection_type='review_alert'").Scan(&item.ID, &item.TenantID, &item.KnowledgeBaseID, &item.ProjectionType, &item.AggregateType, &item.AggregateID, &item.CausationType, &item.CausationID, &payload)
	}); err != nil {
		t.Fatal(err)
	}
	item.Payload = []byte(payload)
	if err := w.Handler(ctx, item); err != nil || calls.Load() != 2 {
		t.Fatalf("committed signal repeated: %d %v", calls.Load(), err)
	}
}

func TestReviewDeadlineRoundTripWithoutInterveningSweepReopens(t *testing.T) {
	db, s, now, scope := fixture(t)
	ctx := context.Background()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	annotate(t, db, scope, &past, 0)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	annotate(t, db, scope, &future, 1)
	annotate(t, db, scope, &past, 2)
	if err := s.Check(ctx, scope); err != nil {
		t.Fatal(err)
	}
	alerts := list(t, s, scope)
	if len(alerts) != 1 || alerts[0].Generation != 2 {
		t.Fatalf("deadline round-trip was lost: %+v", alerts)
	}
}
