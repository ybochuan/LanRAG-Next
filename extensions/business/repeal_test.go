package business

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func repealFixture(t *testing.T) (*Service, *store.DB, *time.Time, Command) {
	t.Helper()
	s, db, clock := fixture(t)
	ctx := t.Context()
	create := cmd("repeal_create", "create-repeal")
	seedVersions(t, db, create.Scope)
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{
		EventID: "source-active", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "source",
		ToState: lifecycle.Active, Reason: "fixture", OccurredAt: *clock,
	}); err != nil {
		t.Fatal(err)
	}
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, doc := range []string{"source", "target"} {
			version := "version-" + doc
			if _, err := tx.Exec(`INSERT INTO ext_document_version VALUES(?,'7','kb',?,?,?,1,?,'','fixture',?)`,
				version, "family-"+doc, doc, textHash(doc), doc+"-active", clock.Format(publicationTime)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO ext_source_snapshot_evidence VALUES('7','kb',?,?,?)`,
				version, `{"header_fields":{"enable_status":"`+textHash(`"enabled"`)+`"}}`, textHash(doc)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, db, clock, create
}

func TestRepealAppliesOnlyAfterApprovalAndDueTimeWithAtomicAudit(t *testing.T) {
	s, db, clock, create := repealFixture(t)
	ctx := t.Context()
	*clock = time.Now().UTC()
	due := clock.Add(time.Hour)
	create.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EffectiveAt: &due}
	created, err := s.Execute(ctx, create)
	if err != nil || created.Status != "pending_review" {
		t.Fatalf("create repeal: %+v, %v", created, err)
	}
	approve := cmd("repeal_approve", "approve-repeal")
	approve.ID, approve.ExpectedRevision = created.ID, created.Revision
	approved, err := s.Execute(ctx, approve)
	if err != nil || approved.Status != "scheduled" {
		t.Fatalf("approve repeal: %+v, %v", approved, err)
	}
	if again, err := s.Execute(ctx, approve); err != nil || again != approved {
		t.Fatalf("approval replay: %+v, %v", again, err)
	}
	scopes := []provenance.Scope{create.Scope}
	check := func(wantState, wantStatus string, events int) {
		t.Helper()
		if err := db.View(ctx, func(tx *sql.Tx) error {
			state, _, err := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "target")
			if err != nil {
				return err
			}
			var status string
			var changes, audits, edges int
			err = tx.QueryRow(`SELECT status,
				(SELECT count(*) FROM ext_lifecycle_events WHERE document_id='target' AND reason='repeal_effective'),
				(SELECT count(*) FROM ext_business_audit WHERE action='repeal_apply'),
				(SELECT count(*) FROM ext_repeal_edges WHERE edge_type='repeals')
				FROM ext_repeal_notices WHERE notice_id=?`, created.ID).Scan(&status, &changes, &audits, &edges)
			if err == nil && (state != wantState || status != wantStatus || changes != events || audits != events || edges != events) {
				t.Errorf("state=%s status=%s changes=%d audits=%d edges=%d", state, status, changes, audits, edges)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SweepRepeals(ctx, scopes); err != nil {
		t.Fatal(err)
	}
	check("active", "scheduled", 0)
	*clock = due
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_repeal_audit BEFORE INSERT ON ext_business_audit
			WHEN NEW.action='repeal_apply' BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepRepeals(ctx, scopes); err == nil {
		t.Fatal("injected audit failure did not propagate")
	}
	check("active", "scheduled", 0)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TRIGGER fail_repeal_audit")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SweepRepeals(ctx, scopes); err != nil {
			t.Fatal(err)
		}
		check("archived", "applied", 1)
	}
	cancel := cmd("repeal_cancel", "cancel-applied-repeal")
	cancel.ID, cancel.ExpectedRevision = created.ID, approved.Revision+1
	if _, err := s.Execute(ctx, cancel); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancelling a completed repeal: %v", err)
	}
}

func TestDueRepealHoldsReadsUntilChangedVersionIsSentForReview(t *testing.T) {
	s, db, clock, create := repealFixture(t)
	ctx := t.Context()
	observe := s.verify
	*clock = time.Now().UTC().Add(-2 * time.Hour)
	due := clock.Add(time.Hour)
	create.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EffectiveAt: &due}
	created, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := retrieval.NewPolicy(db, 7, []string{"kb"})
	if err != nil {
		t.Fatal(err)
	}
	assertBlocked := func(want bool) {
		t.Helper()
		for _, mode := range []string{"rag", "agent"} {
			blocked, err := permit.ExcludedDocumentIDs(ctx, contract.BeforeRetrieveRequest{Context: contract.Context{
				RequestID: "test", SessionID: "test", Mode: mode, TenantID: 7,
				KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"target"},
			}})
			if err != nil || (len(blocked) == 1) != want {
				t.Fatalf("%s permit = %v, %v, want blocked %v", mode, blocked, err, want)
			}
		}
	}
	assertBlocked(false)
	approve := cmd("repeal_approve", "approve-drift")
	approve.ID, approve.ExpectedRevision = created.ID, created.Revision
	if approved, err := s.Execute(ctx, approve); err != nil || approved.Status != "scheduled" {
		t.Fatal(approved, err)
	}
	// A restarted worker uses the persisted deadline and independent current clock.
	restarted := New(db, nil, time.Now)
	restarted.verify = func(context.Context, provenance.Scope, string) (string, error) { return "", ErrUnavailable }
	if err := restarted.SweepRepeals(ctx, []provenance.Scope{create.Scope}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unknown evidence = %v", err)
	}
	assertBlocked(true)
	restarted.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if doc == "target" {
			return "new-target-version", nil
		}
		return observe(ctx, scope, doc)
	}
	if err := restarted.SweepRepeals(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	assertBlocked(false)
	rows, err := restarted.List(ctx, Command{Scope: create.Scope, ID: created.ID, Action: "relation_list"})
	if err != nil || len(rows) != 1 || rows[0]["status"] != "needs_review" || rows[0]["applied_at"] != nil {
		t.Fatalf("version drift record: %v, %v", rows, err)
	}
	current, err := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "target"})
	if err != nil || current.State != lifecycle.Active {
		t.Fatalf("version drift unexpectedly retired target: %+v, %v", current, err)
	}
}

func TestRepealRejectsNativeDisabledEvidenceEvenWithActiveExtensionState(t *testing.T) {
	s, db, clock, create := repealFixture(t)
	ctx := t.Context()
	create.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EffectiveAt: clock}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_source_snapshot_evidence SET snapshot_json=? WHERE version_node_id='version-source'`,
			`{"header_fields":{"enable_status":"`+textHash(`"disabled"`)+`"}}`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, create); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled notice creation = %v", err)
	}
}

func TestConcurrentImmediateRepealApprovalReplaysCommittedReceipt(t *testing.T) {
	s, _, clock, create := repealFixture(t)
	ctx := t.Context()
	create.Payload = Payload{DocumentID: "source", TargetDocumentID: "target", EffectiveAt: clock}
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	approve := cmd("repeal_approve", "approve-immediate")
	approve.ID, approve.ExpectedRevision = draft.ID, draft.Revision
	type delayed struct{}
	waiting, resume := make(chan struct{}), make(chan struct{})
	observe := s.verify
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		if ctx.Value(delayed{}) != nil && doc == "target" {
			close(waiting)
			<-resume
			return "", ErrConflict
		}
		return observe(ctx, scope, doc)
	}
	type response struct {
		result Result
		err    error
	}
	later := make(chan response, 1)
	go func() {
		r, err := s.Execute(context.WithValue(ctx, delayed{}, true), approve)
		later <- response{r, err}
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("second request never reached target verification")
	}
	first, err := s.Execute(ctx, approve)
	close(resume)
	if err != nil || first.Status != "applied" {
		t.Fatal(first, err)
	}
	if r := <-later; r.err != nil || r.result != first {
		t.Fatalf("concurrent replay = %+v, %v", r.result, r.err)
	}
}
