package business

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func seedCrossKBTarget(t *testing.T, db interface {
	Write(context.Context, func(*sql.Tx) error) error
}, kb, doc, version string) {
	t.Helper()
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?, '7', ?, 'document_version', ?, '2026-11-02T00:00:00Z')`, version, kb, doc); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?, '7', ?, ?, '', ?, ?, 1, '2026-11-02T00:00:00Z', '2026-11-02T00:00:00Z')`,
			"family-"+doc, kb, doc, version, version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func seedCrossKBSourceActive(t *testing.T, db *store.DB, ctx context.Context) {
	t.Helper()
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestCrossKnowledgeBaseNormativeRelation(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	seedVersions(t, db, scope)
	seedCrossKBSourceActive(t, db, ctx)
	seedCrossKBTarget(t, db, "kb2", "target2", "version-target2")
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "target2-active", TenantID: 7, KnowledgeBaseID: "kb2", DocumentID: "target2", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target2" {
			return "version-target2", nil
		}
		return "version-" + doc, nil
	}

	c := cmd("relation_declare", "cross-amends")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target2", TargetKnowledgeBaseID: "kb2", EdgeType: "amends"}
	r, err := s.Execute(ctx, c)
	if err != nil || r.Status != "active" {
		t.Fatalf("declare cross-KB amends: %v %v", r, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var targetKB string
		var edgeID, crossEdgeID sql.NullString
		if err := tx.QueryRow(`SELECT target_knowledge_base_id,edge_id,cross_edge_id FROM ext_document_relations WHERE relation_id=?`, r.ID).Scan(&targetKB, &edgeID, &crossEdgeID); err != nil {
			return err
		}
		if targetKB != "kb2" || edgeID.Valid || !crossEdgeID.Valid {
			t.Fatalf("relation storage target=%q edge=%v cross=%v", targetKB, edgeID, crossEdgeID)
		}
		var sameEdges, crossEdges int
		if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM ext_lineage_edge),(SELECT COUNT(*) FROM ext_lineage_edge_cross_kb)`).Scan(&sameEdges, &crossEdges); err != nil {
			return err
		}
		if sameEdges != 0 || crossEdges != 1 {
			t.Fatalf("edges same=%d cross=%d", sameEdges, crossEdges)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.QueryRelated(ctx, scope, "source")
	if err != nil || len(rows) != 1 || rows[0].KnowledgeBaseID != "kb2" || rows[0].DocumentID != "target2" || rows[0].Direction != "outgoing" {
		t.Fatalf("cross query: %v %v", rows, err)
	}
	listed, err := s.List(ctx, Command{Scope: scope, Action: "relation_list", ID: r.ID})
	if err != nil || len(listed) != 1 || listed[0]["target_knowledge_base_id"] != "kb2" || listed[0]["status"] != "active" {
		t.Fatalf("cross relation not listed from source KB: %v %v", listed, err)
	}
	incomingListed, err := s.List(ctx, Command{Scope: provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb2"}, Action: "relation_list", ID: r.ID})
	if err != nil || len(incomingListed) != 1 || incomingListed[0]["target_knowledge_base_id"] != "kb2" {
		t.Fatalf("cross relation not listed from target KB: %v %v", incomingListed, err)
	}
	incoming, err := s.QueryRelated(ctx, provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb2"}, "target2")
	if err != nil || len(incoming) != 1 || incoming[0].KnowledgeBaseID != "kb" || incoming[0].DocumentID != "source" || incoming[0].Direction != "incoming" {
		t.Fatalf("cross incoming query: %v %v", incoming, err)
	}

	c = cmd("relation_revoke", "cross-revoke")
	c.ID, c.ExpectedRevision = r.ID, r.Revision
	if _, err := s.Execute(ctx, c); err != nil {
		t.Fatal(err)
	}
	rows, err = s.QueryRelated(ctx, scope, "source")
	if err != nil || len(rows) != 0 {
		t.Fatalf("revoked cross relation visible: %v %v", rows, err)
	}
	var crossEdges int
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT COUNT(*) FROM ext_lineage_edge_cross_kb`).Scan(&crossEdges)
	}); err != nil || crossEdges != 0 {
		t.Fatalf("cross edge not removed: %d %v", crossEdges, err)
	}
}

func TestCrossKnowledgeBaseRelatedRequiresConfirmation(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	seedVersions(t, db, provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"})
	seedCrossKBSourceActive(t, db, ctx)
	seedCrossKBTarget(t, db, "kb2", "target2", "version-target2")
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "target2-active", TenantID: 7, KnowledgeBaseID: "kb2", DocumentID: "target2", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target2" {
			return "version-target2", nil
		}
		return "version-" + doc, nil
	}

	c := cmd("relation_declare", "cross-related-unconfirmed")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target2", TargetKnowledgeBaseID: "kb2", EdgeType: "related_to"}
	if _, err := s.Execute(ctx, c); err == nil {
		t.Fatal("cross-KB related_to accepted without second confirmation")
	}
	c = cmd("relation_declare", "cross-related")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target2", TargetKnowledgeBaseID: "kb2", EdgeType: "related_to", SecondConfirmation: true, Similarity: .8, Declaration: .8, Structure: .8}
	r, err := s.Execute(ctx, c)
	if err != nil || r.Status != "candidate" {
		t.Fatalf("declare cross-KB related_to: %v %v", r, err)
	}
	rows, err := s.QueryRelated(ctx, provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, "source")
	if err != nil || len(rows) != 0 {
		t.Fatalf("candidate exposed: %v %v", rows, err)
	}
	c = cmd("relation_confirm", "cross-related-confirm")
	c.ID, c.ExpectedRevision = r.ID, r.Revision
	if _, err := s.Execute(ctx, c); err != nil {
		t.Fatal(err)
	}
	rows, err = s.QueryRelated(ctx, provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}, "source")
	if err != nil || len(rows) != 1 || rows[0].KnowledgeBaseID != "kb2" {
		t.Fatalf("confirmed cross related_to: %v %v", rows, err)
	}
}

func TestCrossKnowledgeBaseNavigationRejected(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	seedVersions(t, db, provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"})
	seedCrossKBSourceActive(t, db, ctx)
	seedCrossKBTarget(t, db, "kb2", "target2", "version-target2")
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "target2-active", TenantID: 7, KnowledgeBaseID: "kb2", DocumentID: "target2", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target2" {
			return "version-target2", nil
		}
		return "version-" + doc, nil
	}
	c := cmd("relation_declare", "cross-navigation")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target2", TargetKnowledgeBaseID: "kb2", EdgeType: "cites"}
	if _, err := s.Execute(ctx, c); err == nil {
		t.Fatal("cross-KB navigation relation accepted")
	}
}
