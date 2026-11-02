package lineage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestLineageQueriesSeparateEdgeSemanticsAndGuardCycles(t *testing.T) {
	r, db, reader, _ := registryFixture(t)
	ctx := context.Background()
	first, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	reader.snapshot = fixtureSnapshot(t, "second")
	next, e := r.Refresh(ctx, fixtureScope, "doc")
	if e != nil {
		t.Fatal(e)
	}
	if e = r.RegisterProjectionNode(ctx, fixtureScope, "answer", "chunk", "native-chunk"); e != nil {
		t.Fatal(e)
	}
	if e = r.RecordDerived(ctx, fixtureScope, "answer", next.ID); e != nil {
		t.Fatal(e)
	}
	if e = r.RecordDerived(ctx, fixtureScope, "answer", next.ID); e != nil {
		t.Fatal("duplicate projection", e)
	}
	if e = r.RecordDerived(ctx, provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}, "answer", next.ID); e == nil {
		t.Fatal("cross-scope edge accepted")
	}
	if e = db.Write(ctx, func(tx *sql.Tx) error {
		// Simulate a normative edge produced by a future critical transaction.
		_, e := tx.Exec("INSERT INTO ext_lineage_edge VALUES('fixture-edge','7','kb',?,?,'supersedes','governance_critical','2026-11-01T00:00:00Z')", next.ID, first.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	got, e := r.Query(ctx, fixtureScope, "answer", "provenance")
	if e != nil || len(got) != 1 || got[0].NodeID != next.ID {
		t.Fatal(got, e)
	}
	got, e = r.Query(ctx, fixtureScope, next.ID, "version-ancestry")
	if e != nil || len(got) != 1 || got[0].NodeID != first.ID {
		t.Fatal(got, e)
	}
	got, e = r.Query(ctx, fixtureScope, "answer", "version-ancestry")
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if e = r.RecordDerived(ctx, fixtureScope, next.ID, "answer"); e == nil {
		t.Fatal("projection cycle accepted")
	}
	if e = db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO ext_lineage_edge VALUES('bad-cycle','7','kb',?,'answer','derived_from','reconstructable','2026-11-01T00:00:00Z')", next.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	got, e = r.Query(ctx, fixtureScope, "answer", "provenance")
	if e != nil || len(got) != 1 {
		t.Fatal("stored cycle must terminate", got, e)
	}
}
