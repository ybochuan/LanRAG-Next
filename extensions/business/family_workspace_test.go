package business

import (
	"errors"
	"testing"
)

func TestFamilyWorkspaceSeparatesCurrentRelationshipsAndHistory(t *testing.T) {
	s, _, create := familyFixture(t)
	ctx := t.Context()
	execute := func(c Command) Result {
		t.Helper()
		result, err := s.Execute(ctx, c)
		if err != nil {
			t.Fatal(c.Action, err)
		}
		return result
	}
	family := execute(create)
	member := cmd("family_member_add", "workbench-add")
	member.ID, member.ExpectedRevision, member.Payload.DocumentID = family.ID, family.Revision, "2026"
	family = execute(member)
	declare := cmd("relation_declare", "workbench-declare")
	declare.Payload = Payload{DocumentID: "2025", TargetDocumentID: "2026", EdgeType: "amends"}
	relation := execute(declare)
	query := Command{Scope: create.Scope, ID: family.ID}
	load := func(view string) FamilyWorkspace {
		t.Helper()
		snapshot, err := s.FamilyWorkspace(ctx, query, view)
		if err != nil || snapshot.Family.ID != family.ID {
			t.Fatal(snapshot, err)
		}
		return snapshot
	}
	view := load("relations")
	if len(view.Relations) != 1 || view.Relations[0].ID != relation.ID ||
		view.Relations[0].DocumentID != "2025" || view.Relations[0].TargetDocumentID != "2026" || view.Relations[0].Scope != "external" {
		t.Fatal("incoming relationship was not retained with its direction", view)
	}
	member.OperationID, member.ExpectedRevision, member.Payload.DocumentID = "join-2025", family.Revision, "2025"
	family = execute(member)
	if view = load("relations"); len(view.Relations) != 1 || view.Relations[0].Scope != "internal" {
		t.Fatal("two joined endpoints must form an internal relationship", view)
	}
	revoke := cmd("relation_revoke", "workbench-revoke")
	revoke.ID, revoke.ExpectedRevision = relation.ID, relation.Revision
	execute(revoke)
	if view = load("relations"); len(view.Relations) != 0 {
		t.Fatal("revoked relation remains current", view)
	}
	if view = load("history"); len(view.Relations) != 1 || view.Relations[0].Status != "revoked" || view.Relations[0].Current {
		t.Fatal("revoked relation is missing from history", view)
	}
	remove := cmd("family_member_remove", "workbench-remove")
	remove.ID, remove.ExpectedRevision, remove.Payload.DocumentID, remove.Payload.Reason = family.ID, family.Revision, "2025", "reorganized"
	execute(remove)
	if view = load("removed"); len(view.Members) != 1 || view.Members[0].DocumentID != "2025" ||
		view.Members[0].Reason != "reorganized" || view.Members[0].RemovedAt == nil {
		t.Fatal("removed member is missing from history", view)
	}
	query.PageSize = 2
	if view = load("activity"); len(view.Activity) != 2 || !view.HasNext ||
		view.Activity[0].Action != "family_member_remove" || view.Activity[1].Action != "relation_revoke" {
		t.Fatal("family activity lost related governance records or pagination", view)
	}
	query.Scope.KnowledgeBaseID = "other"
	if _, err := s.FamilyWorkspace(ctx, query, "activity"); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign family was not treated as missing", err)
	}
}

func TestFamilyWorkspaceRevokedClauseDoesNotBecomeCurrentThroughPins(t *testing.T) {
	s, _, create := clauseFixture(t, "exception_to")
	ctx := t.Context()
	family := cmd("family_create", "clause-family")
	family.Payload.FamilyLabel = "Clause family"
	created, err := s.Execute(ctx, family)
	if err != nil {
		t.Fatal(err)
	}
	add := cmd("family_member_add", "clause-family-member")
	add.ID, add.ExpectedRevision, add.Payload.DocumentID = created.ID, created.Revision, "source"
	if _, err := s.Execute(ctx, add); err != nil {
		t.Fatal(err)
	}
	draft, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	query := Command{Scope: create.Scope, ID: created.ID}
	check := func(view, status string) {
		t.Helper()
		result, err := s.FamilyWorkspace(ctx, query, view)
		if err != nil || len(result.Relations) != 1 || result.Relations[0].Status != status {
			t.Fatal(view, status, result, err)
		}
	}
	check("history", "pending_review")
	confirm := cmd("clause_confirm", "workspace-confirm-clause")
	confirm.ID, confirm.ExpectedRevision = draft.ID, draft.Revision
	confirmed, err := s.Execute(ctx, confirm)
	if err != nil {
		t.Fatal(err)
	}
	check("relations", "active")
	revoke := cmd("clause_revoke", "workspace-revoke-clause")
	revoke.ID, revoke.ExpectedRevision = confirmed.ID, confirmed.Revision
	if _, err := s.Execute(ctx, revoke); err != nil {
		t.Fatal(err)
	}
	check("history", "revoked")
}
