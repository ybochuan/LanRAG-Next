package business

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
)

func TestFamilyReplacementRejectsPastEffectiveAt(t *testing.T) {
	s, _, clock, create := familyReplacementFixture(t)
	past := clock.Add(-time.Minute)
	create.OperationID = "past-effective"
	create.Payload.EffectiveAt = &past
	if _, err := s.Execute(t.Context(), create); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past effective_at accepted: %v", err)
	}
}

func TestPolicyExcludesOldMemberWhenScheduledReplacementIsDue(t *testing.T) {
	ctx := t.Context()
	s, db, clock, create := familyReplacementFixture(t)
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	approve := cmd("family_replace_approve", "approve-policy-due")
	approve.ID, approve.ExpectedRevision, approve.Payload.SecondConfirmation = draft.ID, draft.Revision, true
	if _, err := s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}
	var effectiveMS int64
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT effective_at_ms FROM ext_family_replacements WHERE replacement_id=?`, draft.ID).Scan(&effectiveMS)
	}); err != nil {
		t.Fatal(err)
	}
	effective := time.UnixMilli(effectiveMS).UTC()
	*clock = effective
	policy, err := retrieval.NewPolicy(db, create.Scope.TenantID, []string{create.Scope.KnowledgeBaseID})
	if err != nil {
		t.Fatal(err)
	}
	policy.WithNow(func() time.Time { return effective })
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "due", SessionID: "due", Mode: "rag",
		TenantID: create.Scope.TenantID, KnowledgeBaseIDs: []string{create.Scope.KnowledgeBaseID}, KnowledgeIDs: []string{"source", "target"}}}
	excluded, err := policy.ExcludedDocumentIDs(ctx, req)
	if err != nil || !slices.Contains(excluded, "source") {
		t.Fatalf("due source not excluded: %v %v", excluded, err)
	}
}

func TestFamilyReplacementSyncsAnnotationBoundaries(t *testing.T) {
	ctx := t.Context()
	s, db, clock, create := familyReplacementFixture(t)
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	approve := cmd("family_replace_approve", "approve-effective")
	approve.ID, approve.ExpectedRevision, approve.Payload.SecondConfirmation = draft.ID, draft.Revision, true
	if _, err := s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}
	var effectiveMS int64
	if err := db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT effective_at_ms FROM ext_family_replacements WHERE replacement_id=?`, draft.ID).Scan(&effectiveMS)
	}); err != nil {
		t.Fatal(err)
	}
	effective := time.UnixMilli(effectiveMS).UTC()
	*clock = effective
	if err := s.SweepFamilyReplacements(ctx, []provenance.Scope{create.Scope}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var oldFrom, oldUntil, newFrom, newUntil sql.NullString
		if err := tx.QueryRow(`SELECT effective_from,effective_until FROM ext_document_annotations
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id='source'`,
			create.Scope.TenantID, create.Scope.KnowledgeBaseID).Scan(&oldFrom, &oldUntil); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT effective_from,effective_until FROM ext_document_annotations
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id='target'`,
			create.Scope.TenantID, create.Scope.KnowledgeBaseID).Scan(&newFrom, &newUntil); err != nil {
			return err
		}
		if !oldUntil.Valid {
			t.Fatal("old effective_until is missing")
		}
		until, err := time.Parse(time.RFC3339Nano, oldUntil.String)
		if err != nil || !until.Equal(effective.Add(-time.Nanosecond)) {
			t.Fatalf("old effective_until=%v want %v (err=%v)", oldUntil.String, effective.Add(-time.Nanosecond), err)
		}
		if !newFrom.Valid {
			t.Fatal("new effective_from is missing")
		}
		from, err := time.Parse(time.RFC3339Nano, newFrom.String)
		if err != nil || !from.Equal(effective) {
			t.Fatalf("new effective_from=%v want %v (err=%v)", newFrom.String, effective, err)
		}
		if newUntil.Valid {
			t.Fatalf("new effective_until unexpectedly set: %v", newUntil.String)
		}
		var occurred string
		if err := tx.QueryRow(`SELECT occurred_at FROM ext_lifecycle_events
			WHERE tenant_id=? AND knowledge_base_id=? AND document_id='source' AND event_id=?`,
			create.Scope.TenantID, create.Scope.KnowledgeBaseID, "family-replace-"+draft.ID).Scan(&occurred); err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, occurred)
		if err != nil || !at.Equal(effective) {
			t.Fatalf("lifecycle event occurred_at=%v want %v (err=%v)", occurred, effective, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
