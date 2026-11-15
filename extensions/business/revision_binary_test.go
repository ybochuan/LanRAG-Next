package business

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestBinaryRevisionBindsUploadedDocumentAndPublishesAtomically(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	seedVersions(t, db, scope)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}

	create := cmd("revision_create_binary", "create-binary")
	create.Payload = Payload{DocumentID: "source"}
	proposal, err := s.Execute(ctx, create)
	if err != nil || proposal.Status != "pending_review" {
		t.Fatalf("create binary proposal: %+v %v", proposal, err)
	}
	approve := cmd("revision_approve", "approve-binary")
	approve.ID, approve.ExpectedRevision = proposal.ID, proposal.Revision
	proposal, err = s.Execute(ctx, approve)
	if err != nil || proposal.Status != "approved" {
		t.Fatalf("approve binary proposal: %+v %v", proposal, err)
	}
	bind := cmd("revision_bind_binary", "bind-binary")
	bind.ID, bind.ExpectedRevision = proposal.ID, proposal.Revision
	bind.Payload.TargetDocumentID = "target"
	bound, err := s.Execute(ctx, bind)
	if err != nil || bound.TargetDocumentID != "target" || bound.Revision != proposal.Revision+1 {
		t.Fatalf("bind uploaded binary: %+v %v", bound, err)
	}

	s.WithRemote(&remoteFixture{})
	verified := s.verify
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "version-target-next", nil
		}
		return "version-" + doc, nil
	}
	if _, err := s.Publish(ctx, scope, proposal.ID); err == nil {
		t.Fatal("target version drift was published")
	}
	s.verify = verified
	published, err := s.Publish(ctx, scope, proposal.ID)
	if err != nil || published.Status != "published" || published.TargetDocumentID != "target" {
		t.Fatalf("publish binary: %+v %v", published, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var state string
		if err := tx.QueryRow(`SELECT state FROM ext_lifecycle_current WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='source'`).Scan(&state); err != nil {
			return err
		}
		if state != "superseded" {
			t.Fatalf("source state=%s", state)
		}
		var target string
		return tx.QueryRow(`SELECT target_document_id FROM ext_revision_proposals WHERE proposal_id=?`, proposal.ID).Scan(&target)
	}); err != nil {
		t.Fatal(err)
	}
}
