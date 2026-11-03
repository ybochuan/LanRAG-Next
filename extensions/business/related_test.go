package business

import (
	"context"
	"database/sql"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"testing"
	"time"
)

func TestRelatedCandidatesConfirmationRevocationAndStale(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if _, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	c := cmd("relation_declare", "candidate")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: "related_to", Similarity: 1, Declaration: 1, Structure: 1}
	r, e := s.Execute(ctx, c)
	if e != nil || r.Status != "candidate" {
		t.Fatal(r, e)
	}
	listed, e := s.List(ctx, Command{Scope: scope, Action: "relation_list", ID: r.ID})
	if e != nil || len(listed) != 1 || listed[0]["status"] != "candidate" || listed[0]["target_document_id"] != "target" {
		t.Fatal("candidate not recoverable", listed, e)
	}
	items, e := s.QueryRelated(ctx, scope, "source")
	if e != nil || len(items) != 0 {
		t.Fatal("unconfirmed candidate visible", items, e)
	}
	c = cmd("relation_confirm", "confirm")
	c.ID = r.ID
	c.ExpectedRevision = r.Revision
	r, e = s.Execute(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	items, e = s.QueryRelated(ctx, scope, "source")
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	verified := s.verify
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "version-target-next", nil
		}
		return "version-" + doc, nil
	}
	items, e = s.QueryRelated(ctx, scope, "source")
	if e != nil || len(items) != 0 {
		t.Fatal("old version association returned", items, e)
	}
	s.verify = verified
	c = cmd("relation_revoke", "revoke")
	c.ID = r.ID
	c.ExpectedRevision = r.Revision
	if _, e = s.Execute(ctx, c); e != nil {
		t.Fatal(e)
	}
	items, e = s.QueryRelated(ctx, scope, "source")
	if e != nil || len(items) != 0 {
		t.Fatal(items, e)
	}
	c = cmd("relation_declare", "amend")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: "amends"}
	if _, e = s.Execute(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "target-stale", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "target", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	items, e = s.QueryRelated(ctx, scope, "source")
	if e != nil || len(items) != 0 {
		t.Fatal("stale relation exposed", items, e)
	}
	if e = db.View(ctx, func(tx *sql.Tx) error {
		rows, e := tx.Query(`PRAGMA foreign_key_check`)
		if e != nil {
			return e
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("FK violation")
		}
		return rows.Err()
	}); e != nil {
		t.Fatal(e)
	}
}

func TestRelatedRevocationDuringSourceVerification(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if _, e := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	c := cmd("relation_declare", "declare-race")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: "amends"}
	r, e := s.Execute(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	once := false
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target" && !once {
			once = true
			q := cmd("relation_revoke", "revoke-race")
			q.ID = r.ID
			q.ExpectedRevision = r.Revision
			if _, e = s.Execute(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
		return "version-" + doc, nil
	}
	rows, e := s.QueryRelated(ctx, scope, "source")
	if e != nil || len(rows) != 0 {
		t.Fatal("revoked relationship returned", rows, e)
	}
}
