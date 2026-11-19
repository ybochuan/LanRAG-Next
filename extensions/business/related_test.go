package business

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"testing"
	"time"
)

func TestTemplateEvidenceUsesTheVerifiedRelationRevision(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := t.Context()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	seedVersions(t, db, scope)
	_, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	c := cmd("relation_declare", "template-create")
	c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: "template_for"}
	relation, err := s.Execute(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	edit := store.AttachmentReviewEdit{Tenant: 7, KB: "kb", Actor: "7:owner", Operation: "template-reference", Action: "confirm", Parent: "target", ParentHash: "parent-hash", Document: "source", DocumentHash: "template-hash", RelationID: relation.ID, ExpectedRevision: relation.Revision, Reference: store.AttachmentReference{Title: "Application form", Location: "paragraph 12", Sequence: 2}}
	for range 2 {
		result, err := db.AttachmentReview(ctx, edit)
		if err != nil || result.Revision != 2 {
			t.Fatalf("confirm replay = %+v %v", result, err)
		}
	}
	rows, err := s.QueryRelated(ctx, scope, "target")
	if err != nil || len(rows) != 1 || rows[0].RelationID != relation.ID || rows[0].Revision != 2 || rows[0].Reference == nil || rows[0].Reference.Location != "paragraph 12" {
		t.Fatalf("verified relation = %+v %v", rows, err)
	}
	if err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES('version-other','7','kb','document_version','other','2026-11-02T00:00:00Z')`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES('family-other','7','kb','other','','version-other','version-other',1,'2026-11-02T00:00:00Z','2026-11-02T00:00:00Z')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "other-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "other", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	other := cmd("relation_declare", "other-template-create")
	other.Payload = Payload{DocumentID: "source", TargetDocumentID: "other", EdgeType: "template_for"}
	second, err := s.Execute(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	secondEdit := edit
	secondEdit.RelationID, secondEdit.Parent, secondEdit.Operation, secondEdit.ExpectedRevision = second.ID, "other", "other-reference", 1
	secondEdit.Reference.Location = "paragraph 99"
	if _, err = db.AttachmentReview(ctx, secondEdit); err != nil {
		t.Fatal(err)
	}
	firstRefs, err := s.AttachmentTemplates(ctx, scope, "target")
	if err != nil {
		t.Fatal(err)
	}
	secondRefs, err := s.AttachmentTemplates(ctx, scope, "other")
	if err != nil || len(firstRefs) != 1 || len(secondRefs) != 1 || firstRefs[0].Reference == nil || secondRefs[0].Reference == nil || firstRefs[0].Reference.Location != "paragraph 12" || secondRefs[0].Reference.Location != "paragraph 99" {
		t.Fatalf("per-parent references = %+v %+v %v", firstRefs, secondRefs, err)
	}
	c = cmd("relation_revoke", "template-stale-revoke")
	c.ID = relation.ID
	c.ExpectedRevision = 1
	if _, err = s.Execute(ctx, c); !errors.Is(err, ErrConflict) {
		t.Fatalf("outdated revoke = %v", err)
	}
	edit.Operation = "template-stale-save"
	if _, err = db.AttachmentReview(ctx, edit); !errors.Is(err, store.ErrAttachmentConflict) {
		t.Fatalf("outdated save = %v", err)
	}
	c.OperationID = "template-revoke"
	c.ExpectedRevision = 2
	if _, err = s.Execute(ctx, c); err != nil {
		t.Fatal(err)
	}
	edit.Operation = "template-revoked-save"
	edit.ExpectedRevision = 3
	if _, err = db.AttachmentReview(ctx, edit); !errors.Is(err, store.ErrAttachmentConflict) {
		t.Fatalf("revoked save = %v", err)
	}
	if rows, err = s.QueryRelated(ctx, scope, "target"); err != nil || len(rows) != 0 {
		t.Fatalf("revoked rows = %v %v", rows, err)
	}
}

func TestNavigationRelationsUseBusinessAuditWithoutGovernanceEdges(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := t.Context()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "source", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source", ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) { return "version-" + doc, nil }
	for _, kind := range []string{"cites", "implements", "template_for"} {
		c := cmd("relation_declare", "create-"+kind)
		c.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EdgeType: kind}
		relation, err := s.Execute(ctx, c)
		if err != nil || relation.Status != "active" {
			t.Fatalf("declare %s: %v %v", kind, relation, err)
		}
		items, err := s.QueryRelated(ctx, scope, "target")
		if err != nil || len(items) != 1 || items[0].Direction != "incoming" || items[0].DocumentID != "source" || items[0].EdgeType != kind {
			t.Fatalf("query %s: %v %v", kind, items, err)
		}
		c = cmd("relation_revoke", "revoke-"+kind)
		c.ID, c.ExpectedRevision = relation.ID, relation.Revision
		if _, err = s.Execute(ctx, c); err != nil {
			t.Fatal(err)
		}
		items, err = s.QueryRelated(ctx, scope, "target")
		if err != nil || len(items) != 0 {
			t.Fatalf("revoked relation visible: %v %v", items, err)
		}
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var edges, audits int
		err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM ext_lineage_edge),
			(SELECT COUNT(*) FROM ext_business_audit WHERE action IN ('relation_declare','relation_revoke'))`).Scan(&edges, &audits)
		if err == nil && (edges != 0 || audits != 6) {
			t.Fatalf("governance edges=%d, audit rows=%d", edges, audits)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

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
