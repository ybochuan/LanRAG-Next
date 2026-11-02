package ingest

import (
	"context"
	"database/sql"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleObserverPromotesOnceAndResumesAfterFailedDecision(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	o := NewLifecycleObserver(db, func() time.Time { return now })
	obs := observation()
	s := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	if err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TRIGGER reject_decision BEFORE INSERT ON ext_lifecycle_decisions BEGIN SELECT RAISE(ABORT,'fail'); END")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = o.Observe(ctx, obs); err == nil {
		t.Fatal("decision failure hidden")
	}
	if err = db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("DROP TRIGGER reject_decision"); return err }); err != nil {
		t.Fatal(err)
	}
	if err = o.Observe(ctx, obs); err != nil {
		t.Fatal(err)
	}
	if err = o.Observe(ctx, obs); err != nil {
		t.Fatal(err)
	}
	current, err := lifecycle.NewStore(db).Current(ctx, s)
	if err != nil || current.State != lifecycle.Active {
		t.Fatalf("%+v %v", current, err)
	}
	events, err := lifecycle.NewStore(db).Events(ctx, s)
	if err != nil || len(events) != 2 {
		t.Fatalf("%v %v", events, err)
	}
	obs.DocumentID = "failed-doc"
	obs.ParseStatus = "failed"
	if err = o.Observe(ctx, obs); err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "failed-doc"}); err != sql.ErrNoRows {
		t.Fatal(err)
	}
	// A durable stale state must not be revived by the same completed observation.
	past := now.Add(-time.Second)
	cfg := lifecycle.NewConfiguration(db)
	if err = cfg.PutAnnotations(ctx, s, lifecycle.Annotations{EffectiveUntil: &past}, 1); err != nil {
		t.Fatal(err)
	}
	m := lifecycle.NewMachine(db, func() time.Time { return now })
	if err = m.Sweep(ctx, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb-a"}}); err != nil {
		t.Fatal(err)
	}
	obs = observation()
	if err = o.Observe(ctx, obs); err != nil {
		t.Fatal(err)
	}
	current, _ = lifecycle.NewStore(db).Current(ctx, s)
	if current.State != lifecycle.Stale {
		t.Fatal(current)
	}
}

func TestLifecyclePromotionRejectsObsoleteCompletedObservation(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	completed := observation()
	raw := NewObserver(db)
	if err = raw.Observe(ctx, completed); err != nil {
		t.Fatal(err)
	}
	failed := completed
	failed.ParseStatus = "failed"
	failed.UpdatedAt = failed.UpdatedAt.Add(time.Second)
	if err = raw.Observe(ctx, failed); err != nil {
		t.Fatal(err)
	}
	m := lifecycle.NewMachine(db, time.Now)
	s := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
	if err = m.ObserveCompleted(ctx, s, completed.UpdatedAt); err == nil {
		t.Fatal("obsolete completion promoted")
	}
	got, err := lifecycle.NewStore(db).Current(ctx, s)
	if err != nil || got.State == lifecycle.Active {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDailyScanNeverActivatesScheduledAfterFailedOrProcessing(t *testing.T) {
	for _, status := range []string{"failed", "processing"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			now := time.Now().UTC()
			future := now.Add(time.Hour)
			s := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
			if err = lifecycle.NewConfiguration(db).PutAnnotations(ctx, s, lifecycle.Annotations{EffectiveFrom: &future}, 0); err != nil {
				t.Fatal(err)
			}
			o := NewLifecycleObserver(db, func() time.Time { return now })
			obs := observation()
			if err = o.Observe(ctx, obs); err != nil {
				t.Fatal(err)
			}
			got, _ := lifecycle.NewStore(db).Current(ctx, s)
			if got.State != lifecycle.Scheduled {
				t.Fatal(got)
			}
			obs.ParseStatus = status
			obs.UpdatedAt = obs.UpdatedAt.Add(time.Second)
			if err = o.Observe(ctx, obs); err != nil {
				t.Fatal(err)
			}
			now = future
			m := lifecycle.NewMachine(db, func() time.Time { return now })
			_ = m.Sweep(ctx, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb-a"}})
			got, err = lifecycle.NewStore(db).Current(ctx, s)
			if err != nil || got.State != lifecycle.Scheduled {
				t.Fatalf("new %s observation was ignored: state=%+v err=%v", status, got, err)
			}
		})
	}
}
