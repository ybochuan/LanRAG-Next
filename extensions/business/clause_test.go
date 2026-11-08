package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func clauseFixture(t *testing.T, edge string) (*Service, *store.DB, Command) {
	t.Helper()
	s, db, _, _ := repealFixture(t)
	c := cmd("clause_create", "create-clause")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: edge,
		AppliesWhen:  "During annual review",
		SourceClause: &ClauseAnchor{ChunkID: "source-clause", ContentHash: textHash("source text")},
		TargetClause: &ClauseAnchor{ChunkID: "target-clause", ContentHash: textHash("target text")}}
	if err := db.Write(t.Context(), func(tx *sql.Tx) error {
		for _, doc := range []string{"source", "target"} {
			chunks, _ := json.Marshal([]lineage.ChunkLeaf{{ID: doc + "-clause", ContentHash: textHash(doc + " text")}})
			if _, err := tx.Exec(`UPDATE ext_source_snapshot_evidence
				SET snapshot_json=json_set(snapshot_json,'$.chunks',json(?))
				WHERE tenant_id='7' AND knowledge_base_id='kb' AND version_node_id=?`, string(chunks), "version-"+doc); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, db, c
}

func TestClauseExceptionRequiresExactScopedAnchors(t *testing.T) {
	s, db, create := clauseFixture(t, "exception_to")
	ctx := t.Context()
	for label, alter := range map[string]func(*Command){
		"wrong document": func(c *Command) { c.Payload.SourceClause = c.Payload.TargetClause },
		"changed text": func(c *Command) {
			c.Payload.SourceClause = &ClauseAnchor{ChunkID: "source-clause", ContentHash: textHash("new text")}
		},
		"foreign KB": func(c *Command) { c.Scope.KnowledgeBaseID = "other" },
	} {
		bad := create
		alter(&bad)
		if _, err := s.Execute(ctx, bad); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s: %v", label, err)
		}
	}
	draft, err := s.Execute(ctx, create)
	if err != nil || draft.Status != "pending_review" {
		t.Fatal(draft, err)
	}
	confirm := cmd("clause_confirm", "confirm-clause")
	confirm.ID, confirm.ExpectedRevision = draft.ID, draft.Revision
	accepted, err := s.Execute(ctx, confirm)
	if err != nil || accepted.Status != "active" {
		t.Fatal(accepted, err)
	}
	graph, err := lineage.NewRegistry(db, nil).Graph(ctx, create.Scope, "relations", "")
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].Type != "exception_to" || graph.Edges[0].From != "version-source" {
		t.Fatal(graph.Edges, err)
	}
}

func TestClauseConflictResolutionRollbackConcurrencyAndVersionHistory(t *testing.T) {
	s, db, create := clauseFixture(t, "conflicts_with")
	ctx := t.Context()
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	inverse := create
	inverse.OperationID = "create-inverse"
	inverse.Payload.DocumentID, inverse.Payload.TargetDocumentID = create.Payload.TargetDocumentID, create.Payload.DocumentID
	inverse.Payload.SourceClause, inverse.Payload.TargetClause = create.Payload.TargetClause, create.Payload.SourceClause
	if _, err := s.Execute(ctx, inverse); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate conflict: %v", err)
	}
	confirm := cmd("clause_confirm", "confirm-clause")
	confirm.ID, confirm.ExpectedRevision = draft.ID, draft.Revision
	pending, err := s.Execute(ctx, confirm)
	if err != nil || pending.Status != "awaiting_resolution" {
		t.Fatal(pending, err)
	}
	decide := cmd("clause_resolve", "resolve-clause")
	decide.ID, decide.ExpectedRevision = pending.ID, pending.Revision
	decide.Payload = Payload{Resolution: "prefer_source", Text: "Use the annual review clause in this case."}
	changeSQL := func(query string) {
		t.Helper()
		if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec(query); return err }); err != nil {
			t.Fatal(err)
		}
	}
	changeSQL(`INSERT INTO ext_repeal_notices(tenant_id,knowledge_base_id,notice_id,
		document_id,target_document_id,source_version_id,target_version_id,effective_at_ms,status,revision)
		VALUES('7','kb','due-repeal','source','target','version-source','version-target',1,'scheduled',1)`)
	if _, err := s.Execute(ctx, decide); !errors.Is(err, ErrConflict) {
		t.Fatalf("decision allowed before due repeal completed: %v", err)
	}
	if graph, err := lineage.NewRegistry(db, nil).Graph(ctx, create.Scope, "relations", ""); err != nil || len(graph.Edges) != 0 {
		t.Fatal(graph.Edges, err)
	}
	changeSQL("DELETE FROM ext_repeal_notices WHERE notice_id='due-repeal'")
	changeSQL(`CREATE TRIGGER fail_clause_audit BEFORE INSERT ON ext_business_audit WHEN NEW.action='clause_resolve'
		BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	if _, err := s.Execute(ctx, decide); err == nil {
		t.Fatal("injected audit failure did not rollback the decision")
	}
	list := func() map[string]any {
		t.Helper()
		rows, err := s.List(ctx, Command{Scope: create.Scope, ID: draft.ID, Action: "relation_list"})
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		return rows[0]
	}
	if row := list(); row["status"] != "awaiting_resolution" || row["resolution"] != "" {
		t.Fatal(row)
	}
	changeSQL("DROP TRIGGER fail_clause_audit")
	other := decide
	other.OperationID, other.Payload.Resolution = "resolve-other", "prefer_target"
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, c := range []Command{decide, other} {
		wg.Go(func() { _, err := s.Execute(ctx, c); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if row := list(); successes != 1 || row["status"] != "resolved" || row["resolved_by"] != decide.ActorID {
		t.Fatal(successes, row)
	}
	changeSQL(`INSERT INTO ext_lineage_node VALUES('version-target-next','7','kb','document_version','target','2026-11-02T00:01:00Z');
		UPDATE ext_document_version_family SET current_version_node_id='version-target-next' WHERE document_id='target'`)
	observe := s.verify
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "version-target-next", nil
		}
		return observe(ctx, scope, doc)
	}
	if row := list(); row["pins_current"] != false || row["status"] != "resolved" || row["decision_note"] == "" {
		t.Fatal(row)
	}
	graph, err := lineage.NewRegistry(db, nil).Graph(ctx, create.Scope, "relations", "")
	if err != nil || len(graph.Edges) != 0 {
		t.Fatal(graph.Edges, err)
	}
	revoke := cmd("clause_revoke", "revoke-clause")
	revoke.ID, revoke.ExpectedRevision = draft.ID, pending.Revision+1
	if got, err := s.Execute(ctx, revoke); err != nil || got.Status != "revoked" || list()["decision_note"] == "" {
		t.Fatal(got, err)
	}
}
