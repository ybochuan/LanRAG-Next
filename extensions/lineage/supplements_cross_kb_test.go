package lineage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func crossKBSupplementFixture(t *testing.T) (*Supplements, *store.DB) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "cross-supp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	reader := &multiReader{
		bodies: map[string]string{"child": "child", "parent": "parent"},
		t:      t,
	}
	events := lifecycle.NewStore(db)
	for _, item := range []struct {
		kb, doc string
	}{
		{"kb-child", "child"},
		{"kb-parent", "parent"},
	} {
		if _, err = events.Append(ctx, lifecycle.Event{
			EventID:         "event-" + item.doc,
			TenantID:        7,
			KnowledgeBaseID: item.kb,
			DocumentID:      item.doc,
			ToState:         lifecycle.Active,
			Reason:          "fixture",
			OccurredAt:      time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return NewSupplements(db, NewRegistry(db, reader), time.Now), db
}

func TestSupplementCrossKBDeclarationAndAssessment(t *testing.T) {
	s, db := crossKBSupplementFixture(t)
	ctx := context.Background()
	cmd := SupplementCommand{
		Scope:                  dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb-child", DocumentID: "child"},
		ActorID:                "actor",
		OperationID:            "cross-declare",
		Parents:                []string{"parent"},
		ParentKnowledgeBaseIDs: []string{"kb-parent"},
	}
	receipt, err := s.Put(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Revision != 1 || receipt.Status != "active" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	view, err := s.Get(ctx, cmd.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Parents) != 1 || view.Parents[0] != "parent" || len(view.ParentKnowledgeBaseIDs) != 1 || view.ParentKnowledgeBaseIDs[0] != "kb-parent" {
		t.Fatalf("unexpected view: %+v", view)
	}
	if len(view.Assessment.Parents) != 1 || view.Assessment.Parents[0].KnowledgeBaseID != "kb-parent" {
		t.Fatalf("unexpected assessment: %+v", view.Assessment)
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"ext_supplement_cross_parents", "ext_supplement_cross_edges", "ext_supplement_cross_edge_facts"} {
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				t.Fatalf("%s count=%d", table, count)
			}
		}
		var sameParents, sameEdges int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_supplement_parents").Scan(&sameParents); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_lineage_edge WHERE edge_type='supplements'").Scan(&sameEdges); err != nil {
			return err
		}
		if sameParents != 0 || sameEdges != 0 {
			t.Fatalf("cross declaration wrote same-KB rows: parents=%d edges=%d", sameParents, sameEdges)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSupplementCrossKBCycleRejected(t *testing.T) {
	s, _ := crossKBSupplementFixture(t)
	ctx := context.Background()
	child := SupplementCommand{
		Scope:                  dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb-child", DocumentID: "child"},
		ActorID:                "actor",
		OperationID:            "cross-child",
		Parents:                []string{"parent"},
		ParentKnowledgeBaseIDs: []string{"kb-parent"},
	}
	if _, err := s.Put(ctx, child); err != nil {
		t.Fatal(err)
	}
	parent := SupplementCommand{
		Scope:                  dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb-parent", DocumentID: "parent"},
		ActorID:                "actor",
		OperationID:            "cross-parent",
		Parents:                []string{"child"},
		ParentKnowledgeBaseIDs: []string{"kb-child"},
	}
	if _, err := s.Put(ctx, parent); err != ErrConflict {
		t.Fatalf("cross-KB cycle accepted: %v", err)
	}
}
