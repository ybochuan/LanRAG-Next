package business

import (
	"database/sql"
	"testing"
)

func TestFamilyMemberChangeQueuesProjectionAutomatically(t *testing.T) {
	s, db, _, _ := repealFixture(t)
	ctx := t.Context()
	create := cmd("family_create", "projection-family")
	create.Payload.FamilyLabel = "Business family"
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "projection-member")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "target"
	if _, err := s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_projection_outbox
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND projection_type='family_tag'
			AND aggregate_id='target' AND causation_type='business_operation'`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("automatic projection count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	remove := cmd("family_member_remove", "projection-remove")
	remove.ID, remove.ExpectedRevision, remove.Payload.DocumentID, remove.Payload.Reason = family.ID, family.Revision+1, "target", "test"
	if _, err := s.Execute(ctx, remove); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_projection_outbox
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND projection_type='family_tag'
			AND aggregate_id='target' AND causation_type='business_operation'`).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			t.Fatalf("projection after remove count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
