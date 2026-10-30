package taggov

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
)

type staticCatalog struct{ tags []Tag }

func (s staticCatalog) Catalog(context.Context, Scope) ([]Tag, error) { return s.tags, nil }

func TestProjectorUsesCurrentTruthAndDurablyRecordsFailures(t *testing.T) {
	ctx := context.Background()
	db := tagDatabase(t)
	lc := lifecycle.NewStore(db)
	first := lifecycle.Event{EventID: "first", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a", ToState: "pending_review", Reason: "accepted", OccurredAt: time.Now().UTC()}
	if _, err := lc.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.EventID = "second"
	second.FromState = first.ToState
	second.ToState = "active"
	if _, err := lc.Append(ctx, second); err != nil {
		t.Fatal(err)
	}
	s := fixtureStore()
	w, err := NewWriter(s, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	mapping := NewMapping(db, staticCatalog{[]Tag{s.tags["active"]}})
	p := NewProjector(db, lc, mapping, w)
	item, err := outbox.NewStore(db).Claim(ctx, time.Now().UTC().Add(time.Second), time.Minute)
	if err != nil || item == nil || item.CausationID != "first" {
		t.Fatalf("old item=%v %v", item, err)
	}
	if err := p.Handle(ctx, *item); err != nil {
		t.Fatal(err)
	}
	if !equalIDs(s.ids, []string{"business-a", "business-b", "finance", "active"}) {
		t.Fatalf("stale payload applied: %v", s.ids)
	}
	s.failWrite = true
	if err := p.Handle(ctx, *item); err == nil {
		t.Fatal("failed projection acknowledged")
	}
	current, err := lc.Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"})
	if err != nil || current.State != "active" {
		t.Fatalf("truth changed=%+v %v", current, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_tag_sync_failures WHERE outbox_id=?", item.ID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("failure count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
