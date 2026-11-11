package business

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func familyFixture(t *testing.T) (*Service, *store.DB, Command) {
	t.Helper()
	s, db, clock := fixture(t)
	ctx := t.Context()
	for _, doc := range []struct{ id, kb string }{{"2024", "kb"}, {"2025", "kb"}, {"2026", "kb"}, {"foreign", "other"}} {
		event := lifecycle.Event{EventID: "event-" + doc.id, TenantID: 7, KnowledgeBaseID: doc.kb,
			DocumentID: doc.id, ToState: lifecycle.Active, Reason: "test", OccurredAt: *clock}
		if _, err := lifecycle.NewStore(db).Append(ctx, event); err != nil {
			t.Fatal(err)
		}
		if doc.id == "2024" {
			event.EventID, event.FromState, event.ToState = "archive-2024", lifecycle.Active, "archived"
			if _, err := lifecycle.NewStore(db).Append(ctx, event); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Write(ctx, func(tx *sql.Tx) error {
			now := clock.UTC().Format(time.RFC3339Nano)
			node := "version-" + doc.id
			if _, err := tx.Exec(`INSERT INTO ext_lineage_node VALUES(?,'7',?,'document_version',?,?)`, node, doc.kb, doc.id, now); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO ext_document_version_family VALUES(?,'7',?,?,'',?,?,1,?,?)`,
				"observation-"+doc.id, doc.kb, doc.id, node, node, now, now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The fixture substitutes only the remote snapshot, leaving all SQLite scope,
	// compare-and-set, idempotency and audit checks in the real service.
	s.verify = func(_ context.Context, _ provenance.Scope, doc string) (string, error) {
		return "version-" + doc, nil
	}
	create := cmd("family_create", "create-family")
	create.Payload.FamilyLabel = "  员工手册  "
	return s, db, create
}

func TestBusinessFamilyManualMembershipKeepsObservationAndLifecycle(t *testing.T) {
	s, db, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil || family.ID == "" || family.Revision != 1 {
		t.Fatal(family, err)
	}
	if again, err := s.Execute(ctx, create); err != nil || again != family {
		t.Fatal("create receipt changed", again, err)
	}
	for i, id := range []string{"2024", "2025", "2026"} {
		add := cmd("family_member_add", "add-"+id)
		add.ID, add.ExpectedRevision, add.Payload.DocumentID = family.ID, int64(i+1), id
		added, err := s.Execute(ctx, add)
		if err != nil || added.Revision != int64(i+2) {
			t.Fatal(added, err)
		}
		if again, err := s.Execute(ctx, add); err != nil || again != added {
			t.Fatal("add receipt changed", again, err)
		}
	}
	query := Command{Scope: create.Scope, ID: family.ID}
	families, err := s.Families(ctx, query)
	if err != nil || len(families) != 1 || families[0].Label != "员工手册" || families[0].Revision != 4 {
		t.Fatal(families, err)
	}
	members, err := s.FamilyMembers(ctx, query)
	if err != nil || len(members) != 3 {
		t.Fatal(members, err)
	}
	for _, member := range members {
		if member.BusinessFamilyID != family.ID || member.ObservationChainID != "observation-"+member.DocumentID ||
			member.ObservedHeadVersionNodeID != "version-"+member.DocumentID {
			t.Fatal("business family reused or rewrote an observation chain", member)
		}
	}
	remove := cmd("family_member_remove", "remove-2024")
	remove.ID, remove.ExpectedRevision, remove.Payload.DocumentID = family.ID, 4, "2024"
	if removed, err := s.Execute(ctx, remove); err != nil || removed.Revision != 5 {
		t.Fatal(removed, err)
	}
	unassigned, err := s.FamilyMembers(ctx, Command{Scope: query.Scope})
	if err != nil || len(unassigned) != 1 || unassigned[0].DocumentID != "2024" || unassigned[0].BusinessFamilyID != "" {
		t.Fatal(unassigned, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var status, reason string
		if err := tx.QueryRow(`SELECT status,reason FROM ext_business_family_member_history
			WHERE tenant_id='7' AND knowledge_base_id='kb' AND business_family_id=? AND document_id='2024'`, family.ID).Scan(&status, &reason); err != nil {
			return err
		}
		if status != "removed" || reason != "member_removed" {
			t.Fatalf("membership removal missing from history: %s %s", status, reason)
		}
		state, _, err := store.ReadLifecycleCurrentTx(ctx, tx, 7, "kb", "2024")
		if err != nil || state != "archived" {
			t.Fatalf("membership changed lifecycle: %s %v", state, err)
		}
		var edges int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_lineage_edge`).Scan(&edges); err != nil || edges != 0 {
			t.Fatalf("membership wrote lineage edges: %d %v", edges, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rejoin := cmd("family_member_add", "rejoin-2024")
	rejoin.ID, rejoin.ExpectedRevision, rejoin.Payload.DocumentID = family.ID, 5, "2024"
	if added, err := s.Execute(ctx, rejoin); err != nil || added.Revision != 6 {
		t.Fatal(added, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var active, audit int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_business_family_member_history
			WHERE business_family_id=? AND document_id='2024' AND status='active'`, family.ID).Scan(&active); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM ext_business_audit WHERE operation_id IN ('add-2024','remove-2024','rejoin-2024')`).Scan(&audit); err != nil {
			return err
		}
		if active != 1 || audit != 3 {
			t.Fatalf("rejoin membership=%d audit=%d", active, audit)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBusinessFamilyRejectsCrossScopeConcurrentAndUnauditedMembership(t *testing.T) {
	s, db, create := familyFixture(t)
	ctx := t.Context()
	family, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	add := func(doc string) Command {
		c := cmd("family_member_add", "add-"+doc)
		c.ID, c.ExpectedRevision, c.Payload.DocumentID = family.ID, 1, doc
		return c
	}
	observe := s.verify
	observed := false
	s.verify = func(ctx context.Context, scope provenance.Scope, doc string) (string, error) {
		observed = true
		return observe(ctx, scope, doc)
	}
	if _, err := s.Execute(ctx, add("foreign")); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign document accepted", err)
	}
	foreign := add("foreign")
	foreign.Scope.KnowledgeBaseID = "other"
	if _, err := s.Execute(ctx, foreign); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign family accepted", err)
	}
	stale := add("2024")
	stale.ExpectedRevision = 2
	if _, err := s.Execute(ctx, stale); !errors.Is(err, ErrConflict) || observed {
		t.Fatal("invalid membership attempted a source observation", observed, err)
	}
	s.verify = observe
	outcomes := make(chan error, 2)
	for _, id := range []string{"2024", "2025"} {
		go func(doc string) {
			_, err := s.Execute(ctx, add(doc))
			outcomes <- err
		}(id)
	}
	first, second := <-outcomes, <-outcomes
	if !((first == nil && errors.Is(second, ErrConflict)) || (second == nil && errors.Is(first, ErrConflict))) {
		t.Fatal("expected exactly one compare-and-set winner", first, second)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_family_audit BEFORE INSERT ON ext_business_audit
			WHEN NEW.operation_id='add-2026' BEGIN SELECT RAISE(ABORT,'audit failure'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c := add("2026")
	c.ExpectedRevision = 2
	if _, err := s.Execute(ctx, c); err == nil {
		t.Fatal("unaudited membership was committed")
	}
	families, err := s.Families(ctx, Command{Scope: create.Scope})
	if err != nil || len(families) != 1 || families[0].Revision != 2 {
		t.Fatal("failed audit changed family revision", families, err)
	}
	members, err := s.FamilyMembers(ctx, Command{Scope: create.Scope, ID: family.ID})
	if err != nil || len(members) != 1 || members[0].DocumentID == "2026" {
		t.Fatal("failed audit changed members", members, err)
	}
	secondFamily := create
	secondFamily.OperationID = "second-family"
	secondFamily.Payload.FamilyLabel = "另一份手册"
	other, err := s.Execute(ctx, secondFamily)
	if err != nil {
		t.Fatal(err)
	}
	move := add(members[0].DocumentID)
	move.ID, move.OperationID = other.ID, "implicit-move"
	if _, err := s.Execute(ctx, move); !errors.Is(err, ErrConflict) {
		t.Fatal("adding a member to another family implicitly moved it", err)
	}
}
