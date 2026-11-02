package lifecycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func command(t *testing.T, m *Machine, s Scope, id string, from, to State, trigger Trigger) Command {
	t.Helper()
	snap, err := m.Snapshot(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return Command{Event: Event{EventID: id, TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID, DocumentID: s.DocumentID, FromState: from, ToState: to, Reason: string(trigger), OccurredAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)}, Trigger: trigger, ExpectedLastEventID: snap.Current.LastEventID, ExpectedAnnotationRevision: snap.Annotations.Revision, ExpectedPolicyToken: snap.Policy.Token}
}
func mustApply(t *testing.T, m *Machine, c Command) {
	t.Helper()
	if inserted, err := m.Apply(context.Background(), c); err != nil || !inserted {
		t.Fatalf("apply=%v %v", inserted, err)
	}
}

func TestScheduledActivationExpiryAndReview(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	m := NewMachine(db, func() time.Time { return now })
	cfg := NewConfiguration(db)
	s := Scope{7, "kb-a", "doc-a"}
	from := now.Add(time.Hour)
	until := from.Add(time.Hour)
	review := now.Add(time.Minute)
	if err := cfg.PutAnnotations(ctx, s, Annotations{EffectiveFrom: &from, EffectiveUntil: &until, ReviewDue: &review}, 0); err != nil {
		t.Fatal(err)
	}
	mustApply(t, m, command(t, m, s, "init", "", Pending, Initialize))
	if _, err := m.Apply(ctx, command(t, m, s, "early", Pending, Active, ParseCompleted)); !errors.Is(err, ErrTransitionDenied) {
		t.Fatal(err)
	}
	mustApply(t, m, command(t, m, s, "scheduled", Pending, Scheduled, ParseCompleted))
	if _, err := m.Apply(ctx, command(t, m, s, "timer-early", Scheduled, Active, Timer)); !errors.Is(err, ErrTransitionDenied) {
		t.Fatal(err)
	}
	now = review.Add(time.Nanosecond)
	snap, err := m.Snapshot(ctx, s)
	if err != nil || !snap.ReviewOverdue || snap.Current.State != Scheduled {
		t.Fatalf("review=%+v %v", snap, err)
	}
	now = from
	recordCompletedSource(t, m, s)
	activate := command(t, m, s, "activate", Scheduled, Active, Timer)
	mustApply(t, m, activate)
	now = until
	if _, err = m.Apply(ctx, command(t, m, s, "not-expired", Active, Stale, Timer)); !errors.Is(err, ErrTransitionDenied) {
		t.Fatal(err)
	}
	now = until.Add(time.Nanosecond)
	mustApply(t, m, command(t, m, s, "expire", Active, Stale, Timer))
	if err = cfg.PutRules(ctx, PolicyScope{7, "kb-a"}, Rules{ValidDays: 5}, 0); err != nil {
		t.Fatal(err)
	}
	if inserted, err := m.Apply(ctx, activate); err != nil || inserted {
		t.Fatalf("replay after time/policy=%v %v", inserted, err)
	}
	activate.Trigger = ParseCompleted
	if _, err = m.Apply(ctx, activate); !errors.Is(err, ErrEventConflict) {
		t.Fatal(err)
	}
	if got := counts(t, db); got != [3]int{4, 1, 4} {
		t.Fatal(got)
	}
}

func TestPermanentDefaultAndExplicitDatesWin(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	m := NewMachine(db, func() time.Time { return now })
	s := Scope{7, "kb-a", "permanent"}
	mustApply(t, m, command(t, m, s, "p-init", "", Pending, Initialize))
	mustApply(t, m, command(t, m, s, "p-active", Pending, Active, ParseCompleted))
	now = now.AddDate(100, 0, 0)
	if _, err := m.Apply(ctx, command(t, m, s, "p-expire", Active, Stale, Timer)); !errors.Is(err, ErrTransitionDenied) {
		t.Fatal(err)
	}
	cfg := NewConfiguration(db)
	if err := cfg.PutRules(ctx, PolicyScope{7, "kb-a"}, Rules{EffectiveDelayDays: 2, ValidDays: 30, ReviewDays: 7}, 0); err != nil {
		t.Fatal(err)
	}
	s.DocumentID = "defaults"
	mustApply(t, m, command(t, m, s, "d-init", "", Pending, Initialize))
	snap, _ := m.Snapshot(ctx, s)
	if snap.Annotations.EffectiveFrom == nil || !snap.Annotations.EffectiveFrom.Equal(now.AddDate(0, 0, 2)) || !snap.Annotations.EffectiveUntil.Equal(now.AddDate(0, 0, 32)) {
		t.Fatalf("defaults=%+v", snap.Annotations)
	}
	s.DocumentID = "explicit"
	from := now.AddDate(0, 0, -1)
	until := now.AddDate(0, 0, 3)
	if err := cfg.PutAnnotations(ctx, s, Annotations{EffectiveFrom: &from, EffectiveUntil: &until}, 0); err != nil {
		t.Fatal(err)
	}
	mustApply(t, m, command(t, m, s, "e-init", "", Pending, Initialize))
	snap, _ = m.Snapshot(ctx, s)
	if !snap.Annotations.EffectiveFrom.Equal(from) || !snap.Annotations.EffectiveUntil.Equal(until) {
		t.Fatal("explicit dates replaced")
	}
}

func TestMachineConcurrentHeadAndPolicyFences(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Now().UTC()
	m := NewMachine(db, func() time.Time { return now })
	s := Scope{7, "kb-a", "doc"}
	mustApply(t, m, command(t, m, s, "init", "", Pending, Initialize))
	c := command(t, m, s, "activate", Pending, Active, ParseCompleted)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			local := c
			local.Event.EventID = fmt.Sprint("activate-", i)
			ok, err := m.Apply(ctx, local)
			if ok {
				successes.Add(1)
			} else if !errors.Is(err, ErrStateConflict) {
				t.Errorf("loser=%v", err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 || counts(t, db) != [3]int{2, 1, 2} {
		t.Fatalf("success=%d counts=%v", successes.Load(), counts(t, db))
	}
	s.DocumentID = "fences"
	c = command(t, m, s, "f-init", "", Pending, Initialize)
	cfg := NewConfiguration(db)
	if err := cfg.PutRules(ctx, PolicyScope{7, "kb-a"}, Rules{ValidDays: 1}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(ctx, c); !errors.Is(err, ErrConfigurationConflict) {
		t.Fatal(err)
	}
	c = command(t, m, s, "f-init", "", Pending, Initialize)
	if err := cfg.PutAnnotations(ctx, s, Annotations{}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(ctx, c); !errors.Is(err, ErrConfigurationConflict) {
		t.Fatal(err)
	}
}

func TestMachineAllWritesRollback(t *testing.T) {
	for _, table := range []string{"ext_document_annotations", "ext_lifecycle_decisions", "ext_lifecycle_events", "ext_lifecycle_current", "ext_projection_outbox"} {
		t.Run(table, func(t *testing.T) {
			db := database(t)
			ctx := context.Background()
			m := NewMachine(db, time.Now)
			s := Scope{7, "kb-a", "doc"}
			c := command(t, m, s, "init", "", Pending, Initialize)
			if err := db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER reject_write BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected'); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Apply(ctx, c); err == nil {
				t.Fatal("injection did not fail")
			}
			if got := counts(t, db); got != [3]int{} {
				t.Fatal(got)
			}
			if err := db.View(ctx, func(tx *sql.Tx) error {
				for _, name := range []string{"ext_document_annotations", "ext_lifecycle_decisions"} {
					var n int
					if err := tx.QueryRow("SELECT count(*) FROM " + name).Scan(&n); err != nil {
						return err
					}
					if n != 0 {
						t.Errorf("partial %s=%d", name, n)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMachineHeadPreventsABAAndScopeLeak(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	m := NewMachine(db, time.Now)
	s := Scope{7, "kb-a", "doc"}
	mustApply(t, m, command(t, m, s, "init", "", Pending, Initialize))
	old := command(t, m, s, "old", Pending, Active, ParseCompleted)
	// Legacy persistence may still be called by M0: the business fence must not
	// mistake an equal state for the exact head that the caller read.
	for _, e := range []Event{{EventID: "legacy-a", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc", FromState: Pending, ToState: Active, Reason: "legacy", OccurredAt: time.Now()}, {EventID: "legacy-b", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc", FromState: Active, ToState: Pending, Reason: "legacy", OccurredAt: time.Now()}} {
		if _, err := NewStore(db).Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Apply(ctx, old); !errors.Is(err, ErrStateConflict) {
		t.Fatal(err)
	}
	foreign := Scope{8, "kb-a", "doc"}
	snap, err := m.Snapshot(ctx, foreign)
	if err != nil || snap.Current.LastEventID != "" || snap.Annotations.Revision != 0 {
		t.Fatalf("scope leak=%+v %v", snap, err)
	}
	foreign = Scope{7, "kb-b", "doc"}
	snap, err = m.Snapshot(ctx, foreign)
	if err != nil || snap.Current.LastEventID != "" || snap.Annotations.Revision != 0 {
		t.Fatalf("KB leak=%+v %v", snap, err)
	}
	if err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO ext_lifecycle_decisions VALUES('legacy-a','8','kb-a','doc','{}','{}','{}','2026-10-31T00:00:00Z')`)
		return err
	}); err == nil {
		t.Fatal("cross-tenant decision FK accepted")
	}
}
