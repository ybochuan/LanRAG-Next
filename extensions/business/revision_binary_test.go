package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
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
	seedBinarySourceEvidence(t, db, scope, `{"body":"original-binary"}`)
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
	if err := assertBinaryPublication(db, proposal.ID, "publish_failed", "binary_source_version_changed", 0, "active", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
	s.verify = verified
	published, err := s.Publish(ctx, scope, proposal.ID)
	if err != nil || published.Status != "published" || published.TargetDocumentID != "target" {
		t.Fatalf("publish binary: %+v %v", published, err)
	}
	if err := assertBinaryPublication(db, proposal.ID, "published", "", 1, "superseded", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryCreateRecoversReceiptAfterSourceDrift(t *testing.T) {
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
	seedBinarySourceEvidence(t, db, scope, `{"body":"original-binary"}`)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	create := cmd("revision_create_binary", "create-before-source-drift")
	create.Payload.DocumentID = "source"
	proposal, err := s.Execute(ctx, create)
	if err != nil || proposal.ID == "" {
		t.Fatalf("create binary proposal: %+v %v", proposal, err)
	}

	observations := 0
	s.verify = func(_ context.Context, _ provenance.Scope, _ string) (string, error) {
		observations++
		return "version-changed", ErrConflict
	}
	got, err := s.Execute(ctx, create)
	if err != nil || got.ID != proposal.ID || got.Revision != proposal.Revision || observations != 0 {
		t.Fatalf("recover original receipt: %+v %v (observations: %d)", got, err, observations)
	}
	changed := create
	changed.Payload.DocumentID = "other-source"
	if _, err := s.Execute(ctx, changed); !errors.Is(err, ErrConflict) || observations != 0 {
		t.Fatalf("changed payload must conflict before verifying the source: %v", err)
	}
	create.OperationID = "new-create-after-source-drift"
	if _, err := s.Execute(ctx, create); !errors.Is(err, ErrConflict) || observations != 1 {
		t.Fatalf("new operation must verify the current source: %v (observations: %d)", err, observations)
	}
}

func TestBinaryPublishUnknownObservationRetainsSourceAndRetries(t *testing.T) {
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
	seedBinarySourceEvidence(t, db, scope, `{"body":"original-binary"}`)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	proposal := createAndBindBinary(t, s, ctx)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "", errors.New("native version ACK unknown")
		}
		return "version-" + doc, nil
	}
	if _, err := s.Publish(ctx, scope, proposal.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown observation should be unavailable, got %v", err)
	}
	if err := assertBinaryPublication(db, proposal.ID, "publish_failed", "binary_source_version_unknown", 0, "active", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	published, err := s.Publish(ctx, scope, proposal.ID)
	if err != nil || published.Status != "published" {
		t.Fatalf("retry after unknown observation: %+v %v", published, err)
	}
	if err := assertBinaryPublication(db, proposal.ID, "published", "", 1, "superseded", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryPublishFinalAuditFailureRollsBack(t *testing.T) {
	s, db, clock := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	seedVersions(t, db, scope)
	seedBinarySourceEvidence(t, db, scope, `{"body":"original-binary"}`)
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	proposal := createAndBindBinary(t, s, ctx)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_publish_audit BEFORE INSERT ON ext_business_audit WHEN NEW.action='revision_published' BEGIN SELECT RAISE(ABORT,'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, scope, proposal.ID); err == nil {
		t.Fatal("final audit failure ignored")
	}
	if err := assertBinaryPublication(db, proposal.ID, "publishing", "", 0, "active", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER fail_publish_audit`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(3 * time.Minute)
	published, err := s.Publish(ctx, scope, proposal.ID)
	if err != nil || published.Status != "published" {
		t.Fatalf("retry after audit rollback: %+v %v", published, err)
	}
	if err := assertBinaryPublication(db, proposal.ID, "published", "", 1, "superseded", `{"body":"original-binary"}`); err != nil {
		t.Fatal(err)
	}
}

func createAndBindBinary(t *testing.T, s *Service, ctx context.Context) Result {
	t.Helper()
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
	proposal, err = s.Execute(ctx, bind)
	if err != nil || proposal.TargetDocumentID != "target" {
		t.Fatalf("bind uploaded binary: %+v %v", proposal, err)
	}
	return proposal
}

func seedBinarySourceEvidence(t *testing.T, db *store.DB, scope provenance.Scope, snapshot string) {
	t.Helper()
	hash := strings.Repeat("a", 64)
	evidence := strings.Repeat("b", 64)
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_document_version(version_node_id,tenant_id,knowledge_base_id,family_id,document_id,content_hash,observation_sequence,lifecycle_event_id,source_uri,source_format,created_at) VALUES('version-source','7','kb','family-source','source',?,1,'source-active','fixture://source','docx','2026-11-02T00:00:00Z')`, hash); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_source_snapshot_evidence(tenant_id,knowledge_base_id,version_node_id,snapshot_json,evidence_hash) VALUES('7','kb','version-source',?,?)`, snapshot, evidence)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = scope
}

func assertBinaryPublication(db *store.DB, proposalID, status, publishError string, edges int, sourceState, snapshot string) error {
	var gotStatus, gotError, target string
	if err := db.View(context.Background(), func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT status,publish_error,target_document_id FROM ext_revision_proposals WHERE proposal_id=?`, proposalID).Scan(&gotStatus, &gotError, &target); err != nil {
			return err
		}
		var gotState string
		if err := tx.QueryRow(`SELECT state FROM ext_lifecycle_current WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='source'`).Scan(&gotState); err != nil {
			return err
		}
		var gotEdges int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_lineage_edge WHERE tenant_id='7' AND knowledge_base_id='kb' AND edge_type='supersedes'`).Scan(&gotEdges); err != nil {
			return err
		}
		var gotSnapshot string
		if err := tx.QueryRow(`SELECT snapshot_json FROM ext_source_snapshot_evidence WHERE tenant_id='7' AND knowledge_base_id='kb' AND version_node_id='version-source'`).Scan(&gotSnapshot); err != nil {
			return err
		}
		if gotStatus != status || gotError != publishError || target != "target" || gotState != sourceState || gotEdges != edges || gotSnapshot != snapshot {
			return errors.New("binary publication state mismatch")
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
