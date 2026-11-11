package business

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func insertLegacyPublication(t *testing.T, db *sql.Tx, scope provenance.Scope, id, family, old, newer, status string, edge bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO ext_revision_proposals
		(tenant_id,knowledge_base_id,proposal_id,source_document_id,source_version_id,family_id,trigger_guidance_ids,
		 proposed_patch,target_content_hash,status,revision,publication_key,target_document_id,created_at,published_at)
		VALUES(?,?,?,?,?,?,'[]','fixture',?,?,2,?,?,?,?)`,
		args(scope, id, old, "version-"+old, family, textHash(newer), status, "key-"+id, newer, now, now)...)
	if err != nil {
		t.Fatal(err)
	}
	if edge {
		_, err := db.Exec(`INSERT INTO ext_lineage_edge VALUES(?,?,?,?,?,'supersedes','governance_critical',?)`,
			append([]any{textHash("key-" + id + "supersedes")}, args(scope, "version-"+newer, "version-"+old, now)...)...)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFamilyBackfillPlansReadOnlyUsesPublishedEdgeAndIgnoresSplit(t *testing.T) {
	s, db, _, fixture := repealFixture(t)
	ctx := t.Context()
	scope := fixture.Scope
	err := db.Write(ctx, func(tx *sql.Tx) error {
		insertLegacyPublication(t, tx, scope, "published", "family-source", "source", "target", "published", true)
		insertLegacyPublication(t, tx, scope, "missing-edge", "family-target", "target", "source", "published", false)
		insertLegacyPublication(t, tx, scope, "split-part", "family-source", "source", "target", "published", false)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`INSERT INTO ext_revision_split_batches
			(tenant_id,knowledge_base_id,batch_id,source_document_id,source_version_id,status,revision,created_at)
			VALUES('7','kb','batch','source','version-source','approved',1,?)`, now); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_revision_split_parts
			(tenant_id,knowledge_base_id,batch_id,proposal_id,title,position,target_version_id)
			VALUES('7','kb','batch','split-part','split part',1,'version-target')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := s.FamilyBackfillPlans(ctx, Command{Scope: scope})
	if err != nil || len(plans) != 2 {
		t.Fatal(plans, err)
	}
	ready, orphan := plans[0], plans[1]
	if ready.LegacyFamilyID != "family-source" || ready.Status != "ready" || ready.HeadVersionNodeID != "version-target" ||
		len(ready.Links) != 1 || ready.Links[0].EdgeID != textHash("key-publishedsupersedes") ||
		!slices.Equal(ready.DocumentIDs, []string{"source", "target"}) ||
		!slices.Equal(ready.ExcludedSplitProposalIDs, []string{"split-part"}) || len(ready.Fingerprint) != 64 {
		t.Fatal("wrong legacy proof", ready)
	}
	if orphan.Status != "needs_review" || len(orphan.Issues) != 1 || orphan.Issues[0].Code != "missing_supersedes" || orphan.Fingerprint != "" {
		t.Fatal("missing edge was offered for application", orphan)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var families, audits int
		err := tx.QueryRow(`SELECT (SELECT count(*) FROM ext_business_document_families),(SELECT count(*) FROM ext_business_audit)`).Scan(&families, &audits)
		if err == nil && (families != 0 || audits != 0) {
			t.Fatal("preview wrote business state")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreign, err := s.FamilyBackfillPlans(context.Background(), Command{Scope: provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}})
	if err != nil || len(foreign) != 0 {
		t.Fatal("preview escaped scope", foreign, err)
	}
}

func TestFamilyBackfillApplyRevalidatesAndCommitsAtomically(t *testing.T) {
	s, db, _, fixture := repealFixture(t)
	ctx := t.Context()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		insertLegacyPublication(t, tx, fixture.Scope, "published", "family-source", "source", "target", "published", true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := s.FamilyBackfillPlans(ctx, Command{Scope: fixture.Scope})
	if err != nil || len(plans) != 1 || plans[0].Status != "ready" {
		t.Fatal(plans, err)
	}
	create := cmd("family_create", "family")
	create.Payload.FamilyLabel = "Reviewed family"
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	confirm := cmd("family_backfill_apply", "confirm")
	confirm.ID, confirm.ExpectedRevision = family.ID, family.Revision
	confirm.Payload = Payload{LegacyFamilyID: plans[0].LegacyFamilyID, BackfillFingerprint: plans[0].Fingerprint}
	if _, err := s.Execute(ctx, confirm); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing confirmation accepted", err)
	}
	confirm.Payload.SecondConfirmation = true
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_revision_proposals SET status='superseded' WHERE proposal_id='published'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, confirm); !errors.Is(err, ErrConflict) {
		t.Fatal("stale preview accepted", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ext_revision_proposals SET status='published' WHERE proposal_id='published';
			CREATE TRIGGER fail_backfill BEFORE INSERT ON ext_business_audit WHEN NEW.action='family_backfill_apply'
			BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(ctx, confirm); err == nil {
		t.Fatal("backfill succeeded without audit")
	}
	check := func(applied bool) {
		t.Helper()
		families, err := s.Families(ctx, Command{Scope: fixture.Scope, ID: family.ID})
		if err != nil || len(families) != 1 {
			t.Fatal(families, err)
		}
		members, err := s.FamilyMembers(ctx, Command{Scope: fixture.Scope, ID: family.ID})
		if err != nil {
			t.Fatal(err)
		}
		if applied {
			if families[0].Revision != 2 || families[0].PublishedVersionNodeID != "version-target" || len(members) != 2 {
				t.Fatal(families, members)
			}
		} else if families[0].Revision != 1 || families[0].PublishedVersionNodeID != "" || len(members) != 0 {
			t.Fatal("audit failure left partial state", families, members)
		}
	}
	check(false)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER fail_backfill`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(ctx, confirm)
	if err != nil || result.Status != "applied" {
		t.Fatal(result, err)
	}
	if replay, err := s.Execute(ctx, confirm); err != nil || replay != result {
		t.Fatal("receipt changed on replay", replay, err)
	}
	check(true)
	if err := db.View(ctx, func(tx *sql.Tx) error {
		_, err := s.publicationSource(ctx, tx, Publication{Scope: fixture.Scope,
			SourceDocumentID: "target", SourceVersionID: "version-target"})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("legacy publisher bypassed confirmed business head while projection was off: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	plans, err = s.FamilyBackfillPlans(ctx, Command{Scope: fixture.Scope})
	if err != nil || len(plans) != 1 || plans[0].MappedBusinessFamilyID != family.ID || plans[0].Status != "applied" {
		t.Fatal(plans, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var observations, events, edges, mappings int
		err := tx.QueryRow(`SELECT (SELECT count(*) FROM ext_document_version), (SELECT count(*) FROM ext_lifecycle_events),
			(SELECT count(*) FROM ext_lineage_edge), (SELECT count(*) FROM ext_business_family_legacy_mappings)`).
			Scan(&observations, &events, &edges, &mappings)
		if err == nil && (observations != 2 || events != 2 || edges != 1 || mappings != 1) {
			t.Fatalf("backfill modified historical facts: %d %d %d %d", observations, events, edges, mappings)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
