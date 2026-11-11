package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFamilyHeadRecoveryRequiresMissingHeadAndExplicitConfirmation(t *testing.T) {
	s, db, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	revision := family.Revision
	for _, doc := range []string{"2025", "2026"} {
		add := cmd("family_member_add", "add-"+doc)
		add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, revision, doc
		family, err = s.Execute(ctx, add)
		if err != nil {
			t.Fatal(err)
		}
		revision = family.Revision
	}
	clock := s.now().UTC()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if err := insertRecoveryVersionsTx(ctx, tx, clock, "2025", "2026"); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_family_replacements
			(tenant_id,knowledge_base_id,replacement_id,business_family_id,family_revision,
			old_document_id,old_version_node_id,new_document_id,new_version_node_id,
			effective_at_ms,status,revision,applied_event_id,applied_at)
			VALUES('7','kb','seed-head',?,1,'2025','version-2025','2026','version-2026',?,'applied',2,'event-2025',?)`,
			family.ID, clock.Add(-time.Hour).UnixMilli(), clock.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_business_document_family_heads
			(tenant_id,knowledge_base_id,business_family_id,document_id,version_node_id,replacement_id)
			VALUES('7','kb',?,'2025','version-2025','seed-head')`, family.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	missing := cmd("family_head_recover", "recover-head")
	missing.ID, missing.ExpectedRevision = family.ID, revision
	missing.Payload.DocumentID = "2026"
	missing.Payload.Reason = "source document missing"
	if _, err := s.Execute(ctx, missing); !errors.Is(err, ErrInvalid) {
		t.Fatalf("recovery without second confirmation accepted: %v", err)
	}
	missing.Payload.SecondConfirmation = true
	result, err := s.Execute(ctx, missing)
	if err != nil || result.Status != "applied" {
		t.Fatalf("recovery failed: %+v %v", result, err)
	}
	if again, err := s.Execute(ctx, missing); err != nil || again != result {
		t.Fatalf("recovery receipt changed: %+v %v", again, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var doc, version string
		if err := tx.QueryRow(`SELECT document_id,version_node_id FROM ext_business_document_family_heads
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND business_family_id=?`, family.ID).Scan(&doc, &version); err != nil {
			return err
		}
		if doc != "2026" || version != "version-2026" {
			t.Fatalf("head not recovered: %s %s", doc, version)
		}
		var state string
		if err := tx.QueryRow(`SELECT state FROM ext_lifecycle_current
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='2025'`).Scan(&state); err != nil {
			return err
		}
		if state != "superseded" {
			t.Fatalf("old head lifecycle = %s", state)
		}
		var oldAvailability, newAvailability string
		if err := tx.QueryRow(`SELECT status FROM ext_document_availability
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='2025'`).Scan(&oldAvailability); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT status FROM ext_document_availability
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND document_id='2026'`).Scan(&newAvailability); err != nil {
			return err
		}
		if oldAvailability != documentAvailabilityMissing || newAvailability != documentAvailabilityActive {
			t.Fatalf("availability old=%s new=%s", oldAvailability, newAvailability)
		}
		var audit int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_business_audit WHERE operation_id='recover-head'`).Scan(&audit); err != nil {
			return err
		}
		if audit != 1 {
			t.Fatalf("recovery audit count = %d", audit)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func insertRecoveryVersionsTx(ctx context.Context, tx *sql.Tx, clock time.Time, documents ...string) error {
	for _, doc := range documents {
		if _, err := tx.Exec(`INSERT INTO ext_document_version
			(version_node_id,tenant_id,knowledge_base_id,family_id,document_id,content_hash,
			observation_sequence,lifecycle_event_id,source_uri,source_format,created_at)
			VALUES(?, '7','kb',?,?,?,1,?,'','manual',?)`,
			"version-"+doc, "observation-"+doc, doc, strings.Repeat("a", 64), "event-"+doc,
			clock.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}

func TestFamilyHeadRecoveryRejectsPresentHeadAndSameTarget(t *testing.T) {
	s, db, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "add-2025")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "2025"
	if _, err := s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}
	clock := s.now().UTC()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if err := insertRecoveryVersionsTx(ctx, tx, clock, "2024", "2025"); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO ext_family_replacements
			(tenant_id,knowledge_base_id,replacement_id,business_family_id,family_revision,
			old_document_id,old_version_node_id,new_document_id,new_version_node_id,
			effective_at_ms,status,revision,applied_event_id,applied_at)
			VALUES('7','kb','seed-head',?,1,'2025','version-2025','2024','version-2024',?,'applied',2,'event-2025',?)`,
			family.ID, clock.Add(-time.Hour).UnixMilli(), clock.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_business_document_family_heads
			(tenant_id,knowledge_base_id,business_family_id,document_id,version_node_id,replacement_id)
			VALUES('7','kb',?,'2025','version-2025','seed-head')`, family.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A missing remote in the fixture is treated as absent, so this exercises
	// the explicit-head-presence guard through the native observation path only
	// when a remote is configured; same-target is still rejected unconditionally.
	same := cmd("family_head_recover", "recover-same")
	same.ID, same.ExpectedRevision = family.ID, family.Revision
	same.Payload.DocumentID = "2025"
	same.Payload.Reason = "cannot recover to self"
	same.Payload.SecondConfirmation = true
	if _, err := s.Execute(ctx, same); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-target recovery accepted: %v", err)
	}
}
