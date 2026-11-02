package lineage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/dependencies"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

type multiReader struct {
	mu     sync.Mutex
	bodies map[string]string
	t      *testing.T
}

func (r *multiReader) Capture(_ context.Context, s provenance.Scope, doc string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body, ok := r.bodies[doc]
	if !ok {
		return Snapshot{}, ErrEvidence
	}
	v := fixtureSnapshot(r.t, body)
	v.TenantID = s.TenantID
	v.KnowledgeBaseID = s.KnowledgeBaseID
	v.DocumentID = doc
	v.Chunks[0].ID = doc + "-chunk"
	v.RootHash = rootDigest(v)
	return v, nil
}
func supplementFixture(t *testing.T) (*Supplements, *store.DB, *multiReader) {
	t.Helper()
	db, e := store.OpenDurable(context.Background(), filepath.Join(t.TempDir(), "supp.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	r := &multiReader{bodies: map[string]string{"child": "child", "parent": "parent", "parent2": "parent2"}, t: t}
	for _, doc := range []string{"child", "parent", "parent2"} {
		if _, e = lifecycle.NewStore(db).Append(context.Background(), lifecycle.Event{EventID: "event-" + doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	return NewSupplements(db, NewRegistry(db, r), time.Now), db, r
}
func command(op string) SupplementCommand {
	return SupplementCommand{Scope: dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"}, ActorID: "actor", OperationID: op, Parents: []string{"parent"}}
}
func TestSupplementDeclarationReplayCASAndRetainedRevocation(t *testing.T) {
	s, db, _ := supplementFixture(t)
	ctx := context.Background()
	cmd := command("declare")
	receipt, e := s.Put(ctx, cmd)
	if e != nil || receipt.Revision != 1 {
		t.Fatal(receipt, e)
	}
	again, e := s.Put(ctx, cmd)
	if e != nil || again != receipt {
		t.Fatal(again, e)
	}
	conflict := cmd
	conflict.Parents = []string{"parent2"}
	if _, e = s.Put(ctx, conflict); e == nil {
		t.Fatal("conflicting retry accepted")
	}
	view, e := s.Get(ctx, cmd.Scope)
	if e != nil || len(view.Parents) != 1 || view.Assessment.Invalid(time.Now()) {
		t.Fatal(view, e)
	}
	revoke := cmd
	revoke.Parents = []string{}
	revoke.OperationID = "revoke"
	revoke.ExpectedRevision = 1
	if _, e = s.Put(ctx, revoke); e != nil {
		t.Fatal(e)
	}
	view, e = s.Get(ctx, cmd.Scope)
	if e != nil || view.Revision != 2 || view.Assessment.Configured {
		t.Fatal(view, e)
	}
	e = db.View(ctx, func(tx *sql.Tx) error {
		for _, entry := range []struct {
			table string
			want  int
		}{{"ext_supplement_declarations", 2}, {"ext_supplement_audit", 2}, {"ext_lineage_edge", 1}, {"ext_supplement_edge_facts", 1}} {
			var count int
			if e := tx.QueryRow("SELECT COUNT(*) FROM " + entry.table).Scan(&count); e != nil {
				return e
			}
			if count != entry.want {
				t.Fatal(entry, count)
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestSupplementSixteenCASWriters(t *testing.T) {
	s, _, _ := supplementFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	conflicts := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Put(context.Background(), command(fmt.Sprint("op-", i)))
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				wins++
			} else if e == ErrConflict {
				conflicts++
			} else {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || conflicts != 15 {
		t.Fatal(wins, conflicts)
	}
}
func TestSupplementCriticalFactsRollbackTogether(t *testing.T) {
	for _, table := range []string{"ext_supplement_declarations", "ext_supplement_parents", "ext_supplement_current", "ext_lineage_edge", "ext_supplement_edge_facts", "ext_supplement_audit"} {
		t.Run(table, func(t *testing.T) {
			s, db, _ := supplementFixture(t)
			if e := db.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec("CREATE TRIGGER stop_supplement BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'fixture'); END")
				return e
			}); e != nil {
				t.Fatal(e)
			}
			if got, e := s.Put(context.Background(), command("fault")); e == nil || got.Revision != 0 {
				t.Fatal(got, e)
			}
			e := db.View(context.Background(), func(tx *sql.Tx) error {
				for _, name := range []string{"ext_supplement_declarations", "ext_supplement_parents", "ext_supplement_current", "ext_lineage_edge", "ext_supplement_edge_facts", "ext_supplement_audit"} {
					var n int
					if e := tx.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&n); e != nil {
						return e
					}
					if n != 0 {
						t.Fatal(name, n)
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestSupplementRejectsInvalidParentsAndCurrentVersions(t *testing.T) {
	s, db, reader := supplementFixture(t)
	ctx := context.Background()
	for _, parents := range [][]string{{"child"}, {"parent", "parent"}, {"missing"}} {
		cmd := command("invalid")
		cmd.Parents = parents
		if _, e := s.Put(ctx, cmd); e == nil {
			t.Fatal(parents)
		}
	}
	if _, e := s.Put(ctx, command("declare")); e != nil {
		t.Fatal(e)
	}
	cycle := command("cycle")
	cycle.Scope.DocumentID = "parent"
	cycle.Parents = []string{"child"}
	if _, e := s.Put(ctx, cycle); e == nil {
		t.Fatal("cycle accepted")
	}
	reader.mu.Lock()
	reader.bodies["parent"] = "edited"
	reader.mu.Unlock()
	if _, e := s.registry.Refresh(ctx, fixtureScope, "parent"); e != nil {
		t.Fatal(e)
	}
	view, e := s.Get(ctx, command("x").Scope)
	if e != nil || !view.Assessment.Invalid(time.Now()) {
		t.Fatal(view, e)
	}
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "parent-stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	cmd := command("invalid-parent")
	cmd.ExpectedRevision = 1
	if _, e = s.Put(ctx, cmd); e == nil {
		t.Fatal("inactive parent accepted")
	}
}

func TestSupplementLifecycleReviewAdvisoryAndParentInvalidation(t *testing.T) {
	s, db, _ := supplementFixture(t)
	ctx := context.Background()
	cmd := command("declare")
	if _, e := s.Put(ctx, cmd); e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-time.Hour)
	if e := lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent"}, lifecycle.Annotations{ReviewDue: &past}, 0); e != nil {
		t.Fatal(e)
	}
	rv := review.NewService(db, time.Now)
	if e := rv.Sweep(ctx, []lifecycle.PolicyScope{{TenantID: 7, KnowledgeBaseID: "kb"}}); e != nil {
		t.Fatal(e)
	}
	alerts, e := rv.List(ctx, outbox.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, 100)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, a := range alerts {
		if a.DocumentID == "child" {
			found = true
		}
	}
	if !found {
		t.Fatal("parent review did not create child advisory", alerts)
	}
	m := lifecycle.NewMachine(db, time.Now)
	if e := m.Reevaluate(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"}); e != nil {
		t.Fatal(e)
	}
	snap, e := m.Snapshot(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
	if e != nil || snap.Current.State != lifecycle.Active || !snap.ReviewOverdue {
		t.Fatal("parent review must be advisory", snap, e)
	}
	if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "parent-invalid", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	if e = m.Reevaluate(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"}); e != nil {
		t.Fatal(e)
	}
	snap, e = m.Snapshot(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"})
	if e != nil || snap.Current.State != lifecycle.Stale {
		t.Fatal("parent invalidation not applied", snap, e)
	}
	if e = m.Reevaluate(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"}); e != nil {
		t.Fatal(e)
	}
}

func TestScheduledChildMayDeclareEffectiveParent(t *testing.T) {
	s, db, _ := supplementFixture(t)
	ctx := context.Background()
	if _, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "scheduled-child", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child", FromState: lifecycle.Active, ToState: lifecycle.Scheduled, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	future := time.Now().Add(time.Hour)
	if e := lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "child"}, lifecycle.Annotations{EffectiveFrom: &future}, 0); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Put(ctx, command("schedule")); e != nil {
		t.Fatal("scheduled child rejected", e)
	}
}

func TestLaggingSupplementHeadCannotPermit(t *testing.T) {
	s, db, _ := supplementFixture(t)
	ctx := context.Background()
	cmd := command("declare")
	old, e := s.Put(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	cmd.ExpectedRevision = 1
	cmd.OperationID = "revoke"
	cmd.Parents = []string{}
	if _, e = s.Put(ctx, cmd); e != nil {
		t.Fatal(e)
	}
	if e = db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec("UPDATE ext_supplement_current SET revision=1,declaration_id=?", old.DeclarationID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, cmd.Scope); e == nil {
		t.Fatal("historical head treated as current governance")
	}
}

func TestSharedAncestorCannotHideDepthEleven(t *testing.T) {
	s, db, r := supplementFixture(t)
	ctx := context.Background()
	ids := []string{"root", "a_short", "tail"}
	for i := 1; i <= 9; i++ {
		ids = append(ids, fmt.Sprintf("z%02d", i))
	}
	for _, doc := range ids {
		r.bodies[doc] = doc
		if _, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	put := func(doc string, parents ...string) error {
		cmd := command("depth-" + doc)
		cmd.Scope.DocumentID = doc
		cmd.Parents = parents
		_, e := s.Put(ctx, cmd)
		return e
	}
	if e := put("a_short", "tail"); e != nil {
		t.Fatal(e)
	}
	next := "a_short"
	for i := 9; i >= 1; i-- {
		doc := fmt.Sprintf("z%02d", i)
		if e := put(doc, next); e != nil {
			t.Fatal(e)
		}
		next = doc
	}
	if e := put("root", "a_short", "z01"); e == nil {
		t.Fatal("shared ancestor concealed depth eleven")
	}
	view, e := s.Get(ctx, dependencies.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "root"})
	if e != nil || view.Revision != 0 {
		t.Fatal("rejected declaration partially committed", view, e)
	}
}

func TestConcurrentParentClosureRequiresAllFreshSources(t *testing.T) {
	s, _, _ := supplementFixture(t)
	ctx := context.Background()
	versions := map[string]Version{}
	for _, doc := range []string{"child", "parent"} {
		v, e := s.registry.Refresh(ctx, fixtureScope, doc)
		if e != nil {
			t.Fatal(e)
		}
		versions[doc] = v
	}
	// Another committed command grows P's closure after the child's collection.
	cmd := command("grow-parent")
	cmd.Scope.DocumentID = "parent"
	cmd.Parents = []string{"parent2"}
	if _, e := s.Put(ctx, cmd); e != nil {
		t.Fatal(e)
	}
	view, e := s.Get(ctx, cmd.Scope)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateSupplementSources(view.Assessment, versions); e == nil {
		t.Fatal("new ancestor accepted without fresh evidence")
	}
	v, e := s.registry.Refresh(ctx, fixtureScope, "parent2")
	if e != nil {
		t.Fatal(e)
	}
	versions["parent2"] = v
	if e = validateSupplementSources(view.Assessment, versions); e != nil {
		t.Fatal("fresh closure denied", e)
	}
}
