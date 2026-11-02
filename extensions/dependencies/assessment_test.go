package dependencies_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func fixture(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, e := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "deps.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	for _, doc := range []string{"child", "parent"} {
		if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "event-" + doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
		e = db.Write(ctx, func(tx *sql.Tx) error {
			if _, e := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,'2026-11-01T00:00:00Z')", doc+"-v", doc); e != nil {
				return e
			}
			if _, e := tx.Exec("INSERT INTO ext_document_version_family VALUES(?,'7','kb',?,'',?,?,1,'2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')", doc+"-family", doc, doc+"-v", doc+"-v"); e != nil {
				return e
			}
			_, e := tx.Exec("INSERT INTO ext_document_version VALUES(?,'7','kb',?,?,?,1,?,'','fixture','2026-11-01T00:00:00Z')", doc+"-v", doc+"-family", doc, strings.Repeat("a", 64), "event-"+doc)
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	declare(t, db, "decl-child", "child", "parent")
	return db
}
func declare(t *testing.T, db *store.DB, id, child, parent string) {
	t.Helper()
	e := db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO ext_supplement_declarations(declaration_id,tenant_id,knowledge_base_id,document_id,revision,child_version_node_id,child_event_id,actor_id,operation_id,payload_hash,status,created_at) VALUES(?,'7','kb',?,1,?,?,'actor',?,?,'active','2026-11-01T00:00:00Z')", id, child, child+"-v", "event-"+child, id, strings.Repeat("b", 64)); e != nil {
			return e
		}
		if _, e := tx.Exec("INSERT INTO ext_supplement_parents VALUES(?,'7','kb',?,?,?,?)", id, child, parent, parent+"-v", "event-"+parent); e != nil {
			return e
		}
		_, e := tx.Exec("INSERT INTO ext_supplement_current VALUES('7','kb',?,1,?)", child, id)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
}
func assess(t *testing.T, db *store.DB) dependencies.Assessment {
	t.Helper()
	var a dependencies.Assessment
	e := db.View(context.Background(), func(tx *sql.Tx) error {
		var e error
		a, e = dependencies.LoadTx(context.Background(), tx, dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestParentDatesReviewAndLifecycleAreIndependent(t *testing.T) {
	db := fixture(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	if a := assess(t, db); !a.Configured || a.Invalid(now) {
		t.Fatal(a)
	}
	config := lifecycle.NewConfiguration(db)
	scope := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent"}
	if e := config.PutAnnotations(context.Background(), scope, lifecycle.Annotations{EffectiveUntil: &future, ReviewDue: &past}, 0); e != nil {
		t.Fatal(e)
	}
	a := assess(t, db)
	if a.Invalid(now) || !a.ReviewOverdue(now) {
		t.Fatal("review deadline invalidated document", a)
	}
	if e := config.PutAnnotations(context.Background(), scope, lifecycle.Annotations{EffectiveUntil: &past}, 1); e != nil {
		t.Fatal(e)
	}
	if !assess(t, db).Invalid(now) {
		t.Fatal("expired parent allowed")
	}
}
func TestParentInvalidationAndVersionChangesRefuseChild(t *testing.T) {
	db := fixture(t)
	now := time.Now()
	if _, e := lifecycle.NewStore(db).Append(context.Background(), lifecycle.Event{EventID: "stale-parent", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: now}); e != nil {
		t.Fatal(e)
	}
	if !assess(t, db).Invalid(now) {
		t.Fatal("inactive parent allowed")
	}
	db2 := fixture(t)
	e := db2.Write(context.Background(), func(tx *sql.Tx) error {
		if _, e := tx.Exec("INSERT INTO ext_lineage_node VALUES('parent-v2','7','kb','document_version','parent','2026-11-01T00:00:00Z')"); e != nil {
			return e
		}
		_, e := tx.Exec("UPDATE ext_document_version_family SET current_version_node_id='parent-v2' WHERE document_id='parent'")
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if !assess(t, db2).Invalid(now) {
		t.Fatal("changed parent version allowed")
	}
}
func TestDependencyCycleAndForeignScopeNeverPermit(t *testing.T) {
	db := fixture(t)
	declare(t, db, "decl-parent", "parent", "child")
	var a dependencies.Assessment
	e := db.View(context.Background(), func(tx *sql.Tx) error {
		var e error
		a, e = dependencies.LoadTx(context.Background(), tx, dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
		return e
	})
	if e == nil && !a.Invalid(time.Now()) {
		t.Fatal("dependency cycle allowed")
	}
	e = db.View(context.Background(), func(tx *sql.Tx) error {
		var e error
		a, e = dependencies.LoadTx(context.Background(), tx, dependencies.Scope{TenantID: 8, KnowledgeBaseID: "kb", DocumentID: "child"})
		return e
	})
	if e != nil || a.Configured {
		t.Fatal("foreign dependency visible", a, e)
	}
}

func TestMissingDeclarationHeadIsNotUnconfigured(t *testing.T) {
	db := fixture(t)
	if e := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.Exec("DELETE FROM ext_supplement_current WHERE document_id='child'")
		return e
	}); e != nil {
		t.Fatal(e)
	}
	e := db.View(context.Background(), func(tx *sql.Tx) error {
		_, e := dependencies.LoadTx(context.Background(), tx, dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
		return e
	})
	if e == nil {
		t.Fatal("lost current declaration silently permitted")
	}
}
