package business

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/lineage"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestFamilyProjectionSwitchIsScopedAndReplayable(t *testing.T) {
	s, db, _, fixture := repealFixture(t)
	ctx := t.Context()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		insertLegacyPublication(t, tx, fixture.Scope, "legacy", "family-source", "source", "target", "published", true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	create := cmd("family_create", "projection-family")
	create.Payload.FamilyLabel = "Business family"
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "projection-member")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, family.Revision, "target"
	member, err := s.Execute(ctx, add)
	if err != nil {
		t.Fatal(err)
	}
	registry := lineage.NewRegistry(db, nil)
	graphMember := func(expect string) {
		t.Helper()
		graph, err := registry.Graph(ctx, fixture.Scope, "version-ancestry", "version-target")
		if err != nil {
			t.Fatal(graph, err)
		}
		for _, node := range graph.Nodes {
			if node.ID == "version-target" && node.BusinessFamilyID == expect &&
				node.ObservationChainID == "family-target" && node.FamilyID == "family-target" {
				return
			}
		}
		t.Fatal(graph.Nodes)
	}
	graphMember("")
	cutover := cmd("family_projection_set", "enable-projection")
	enabled := true
	cutover.Payload = Payload{ProjectionEnabled: &enabled}
	if _, err := s.Execute(ctx, cutover); !errors.Is(err, ErrInvalid) {
		t.Fatal("scope switch skipped confirmation", err)
	}
	cutover.Payload.SecondConfirmation = true
	receipt, err := s.Execute(ctx, cutover)
	if err != nil || receipt.Revision != 1 || receipt.Status != "enabled" {
		t.Fatal(receipt, err)
	}
	if replay, err := s.Execute(ctx, cutover); err != nil || replay != receipt {
		t.Fatal(replay, err)
	}
	graphMember(family.ID)
	workspace, err := s.FamilyWorkspace(ctx, Command{Scope: fixture.Scope, ID: family.ID}, "members")
	if err != nil || workspace.Projection == nil || !workspace.Projection.Enabled || workspace.Projection.Revision != 1 {
		t.Fatalf("workspace projection setting: %+v %v", workspace.Projection, err)
	}
	unrelated, err := s.FamilyProjection(ctx, provenance.Scope{TenantID: 7, KnowledgeBaseID: "other"})
	if err != nil || unrelated.Enabled || unrelated.Revision != 0 {
		t.Fatal(unrelated, err)
	}
	remove := cmd("family_member_remove", "remove-projected")
	remove.ID, remove.ExpectedRevision, remove.Payload.DocumentID = family.ID, member.Revision, "target"
	if _, err := s.Execute(ctx, remove); err != nil {
		t.Fatal(err)
	}
	graphMember("")
	disabled := false
	cutover.OperationID, cutover.ExpectedRevision, cutover.Payload.ProjectionEnabled = "disable-projection", receipt.Revision, &disabled
	if _, err := s.Execute(ctx, cutover); err != nil {
		t.Fatal(err)
	}
	view, err := s.FamilyProjection(ctx, fixture.Scope)
	if err != nil || view.Enabled || view.Revision != 2 {
		t.Fatal(view, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count, unknown int
		err := tx.QueryRow(`SELECT COUNT(*) FILTER(WHERE aggregate_id='target'),
			COUNT(*) FILTER(WHERE aggregate_id='source') FROM ext_projection_outbox
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND projection_type='family_tag'
			AND causation_type='business_operation'`).Scan(&count, &unknown)
		if err == nil && (count != 3 || unknown != 0) {
			t.Fatalf("projection should only queue explicitly managed members; target=%d source=%d", count, unknown)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
