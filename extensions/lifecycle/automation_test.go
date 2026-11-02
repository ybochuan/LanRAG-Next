package lifecycle

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestExpiredPendingAndScheduledGoDirectlyStale(t *testing.T) {
	for _, state := range []State{Pending, Scheduled} {
		t.Run(string(state), func(t *testing.T) {
			db := database(t)
			ctx := context.Background()
			now := time.Now().UTC()
			m := NewMachine(db, func() time.Time { return now })
			s := Scope{7, "kb", "doc"}
			cfg := NewConfiguration(db)
			mustApply(t, m, command(t, m, s, "init", "", Pending, Initialize))
			if state == Scheduled {
				future := now.Add(time.Hour)
				if err := cfg.PutAnnotations(ctx, s, Annotations{EffectiveFrom: &future}, 1); err != nil {
					t.Fatal(err)
				}
				mustApply(t, m, command(t, m, s, "scheduled", Pending, Scheduled, ParseCompleted))
			}
			past := now.Add(-time.Second)
			a, err := cfg.Annotations(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			a.EffectiveFrom = nil
			a.EffectiveUntil = &past
			if err = cfg.PutAnnotations(ctx, s, a, a.Revision); err != nil {
				t.Fatal(err)
			}
			if err = m.Sweep(ctx, []PolicyScope{{7, "kb"}}); err != nil {
				t.Fatal(err)
			}
			snap, _ := m.Snapshot(ctx, s)
			if snap.Current.State != Stale {
				t.Fatal(snap.Current)
			}
			if err = m.Sweep(ctx, []PolicyScope{{7, "kb"}}); err != nil {
				t.Fatal(err)
			}
			if n := counts(t, db)[0]; n != map[bool]int{true: 3, false: 2}[state == Scheduled] {
				t.Fatal(n)
			}
		})
	}
}

func TestSweepActivatesOnlyDueScheduledAndOnlyConfiguredKB(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Now().UTC()
	m := NewMachine(db, func() time.Time { return now })
	cfg := NewConfiguration(db)
	for _, kb := range []string{"kb-a", "kb-b"} {
		s := Scope{7, kb, "doc"}
		future := now.Add(time.Hour)
		if err := cfg.PutAnnotations(ctx, s, Annotations{EffectiveFrom: &future}, 0); err != nil {
			t.Fatal(err)
		}
		mustApply(t, m, command(t, m, s, kb+"-init", "", Pending, Initialize))
		mustApply(t, m, command(t, m, s, kb+"-scheduled", Pending, Scheduled, ParseCompleted))
		recordCompletedSource(t, m, s)
	}
	now = now.Add(time.Hour)
	if err := m.Sweep(ctx, []PolicyScope{{7, "kb-a"}}); err != nil {
		t.Fatal(err)
	}
	a, _ := m.Snapshot(ctx, Scope{7, "kb-a", "doc"})
	b, _ := m.Snapshot(ctx, Scope{7, "kb-b", "doc"})
	if a.Current.State != Active || b.Current.State != Scheduled {
		t.Fatalf("a=%+v b=%+v", a.Current, b.Current)
	}
}

func recordCompletedSource(t *testing.T, m *Machine, s Scope) {
	t.Helper()
	ctx := context.Background()
	if err := m.db.Write(ctx, func(tx *sql.Tx) error {
		args := append(s.args(), m.now().UTC().Format(time.RFC3339Nano), m.now().UnixMilli())
		result, err := tx.Exec(`INSERT INTO ext_ingest_observations(tenant_id,knowledge_base_id,document_id,parse_status,source_updated_at,observed_at,origin) VALUES(?,?,?,'completed',?,?,'reconcile')`, args...)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO ext_ingest_current(tenant_id,knowledge_base_id,document_id,last_observation_id,last_seen_at) VALUES(?,?,?,?,?)`, append(s.args(), id, m.now().UnixMilli())...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSweepRefusesScheduledActivationWithMissingObservation(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Now().UTC()
	m := NewMachine(db, func() time.Time { return now })
	s := Scope{7, "kb", "doc"}
	future := now.Add(time.Hour)
	if err := NewConfiguration(db).PutAnnotations(ctx, s, Annotations{EffectiveFrom: &future}, 0); err != nil {
		t.Fatal(err)
	}
	mustApply(t, m, command(t, m, s, "init", "", Pending, Initialize))
	mustApply(t, m, command(t, m, s, "scheduled", Pending, Scheduled, ParseCompleted))
	now = future
	_ = m.Sweep(ctx, []PolicyScope{{7, "kb"}})
	got, _ := m.Snapshot(ctx, s)
	if got.Current.State != Scheduled {
		t.Fatal("missing source observation was treated as completed", got.Current)
	}
}
