package ingest

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func observation() Observation {
	return Observation{Snapshot: Snapshot{Scope: Scope{7, "kb-a"}, DocumentID: "doc-a", ParseStatus: "completed", UpdatedAt: time.Date(2026, 10, 30, 0, 0, 0, 123, time.UTC)}, Origin: "reconcile", ObservedAt: time.Date(2026, 10, 30, 0, 0, 1, 0, time.UTC)}
}

func TestPersistentObserverDeduplicatesRejectsStaleAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agentrag_ext.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewObserver(db)
	first := observation()
	if err := s.Observe(ctx, first); err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ObservedAt = first.ObservedAt.Add(time.Second)
	if err := s.Observe(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	stale := first
	stale.UpdatedAt = first.UpdatedAt.Add(-time.Second)
	if err := s.Observe(ctx, stale); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("stale=%v", err)
	}
	conflict := first
	conflict.ParseStatus = "failed"
	if err := s.Observe(ctx, conflict); !errors.Is(err, ErrObservationConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		for table, want := range map[string]int{"ext_ingest_observations": 1, "ext_ingest_current": 1, "ext_lifecycle_events": 0, "ext_projection_outbox": 0} {
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
				return err
			}
			if count != want {
				t.Errorf("%s=%d want=%d", table, count, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err := NewObserver(reopened).Current(ctx, first.Scope, first.DocumentID)
	if err != nil || current.ParseStatus != "completed" || current.Origin != "reconcile" || !current.UpdatedAt.Equal(first.UpdatedAt) || !current.ObservedAt.Equal(duplicate.ObservedAt) {
		t.Fatalf("current=%+v %v", current, err)
	}
	if _, err := NewObserver(reopened).Current(ctx, Scope{8, "kb-a"}, first.DocumentID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scope leak=%v", err)
	}
	newer := first
	newer.UpdatedAt = first.UpdatedAt.Add(time.Second)
	newer.ObservedAt = duplicate.ObservedAt.Add(time.Second)
	newer.ParseStatus = "failed"
	if err := NewObserver(reopened).Observe(ctx, newer); err != nil {
		t.Fatal(err)
	}
	current, err = NewObserver(reopened).Current(ctx, newer.Scope, newer.DocumentID)
	if err != nil || current.ParseStatus != "failed" {
		t.Fatalf("newer current=%+v %v", current, err)
	}
}

func TestObserverRollsBackBothSQLStages(t *testing.T) {
	for _, table := range []string{"ext_ingest_observations", "ext_ingest_current"} {
		t.Run(table, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "agentrag_ext.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER ext_abort BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected'); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := NewObserver(db).Observe(ctx, observation()); err == nil {
				t.Fatal("failed observer acknowledged")
			}
			if err := db.View(ctx, func(tx *sql.Tx) error {
				var count int
				if err := tx.QueryRow("SELECT COUNT(*) FROM ext_ingest_observations").Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					t.Error("partial observation committed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
