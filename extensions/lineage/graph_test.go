package lineage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestGraphPreservesDiamondEdgesAndScopeAndCycles(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	ctx := context.Background()
	err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{"a", "b", "c", "d"} {
			if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','document_version',?,'2026-11-05T00:00:00Z')", id, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES('foreign','8','kb','document_version','private','2026-11-05T00:00:00Z')"); err != nil {
			return err
		}
		for i, pair := range [][2]string{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}, {"d", "a"}} {
			if _, err := tx.Exec("INSERT INTO ext_lineage_edge VALUES(?,'7','kb',?,?,'supersedes','governance_critical','2026-11-05T00:00:00Z')", fmt.Sprint(i), pair[0], pair[1]); err != nil {
				return err
			}
		}
		_, err := tx.Exec("INSERT INTO ext_lineage_edge VALUES('derived','7','kb','a','d','derived_from','reconstructable','2026-11-05T00:00:00Z')")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(ctx, fixtureScope, "version-ancestry", "a")
	if err != nil || len(graph.Nodes) != 4 || len(graph.Edges) != 5 || graph.Truncated {
		t.Fatalf("diamond/cycle: %+v %v", graph, err)
	}
	for _, node := range graph.Nodes {
		if node.ID == "foreign" {
			t.Fatal("foreign node leaked")
		}
	}
	graph, err = r.Graph(ctx, fixtureScope, "provenance", "a")
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].From != "a" || graph.Edges[0].To != "d" {
		t.Fatalf("derived direction or edge types: %+v %v", graph, err)
	}
	if _, err = r.Graph(ctx, provenance.Scope{TenantID: 8, KnowledgeBaseID: "kb"}, "provenance", "a"); err == nil {
		t.Fatal("foreign root admitted")
	}
	if _, err = r.Graph(ctx, fixtureScope, "unsupported", ""); err == nil {
		t.Fatal("unknown graph mode")
	}
}

func TestGraphReportsBoundedNodeWindow(t *testing.T) {
	r, db, _, _ := registryFixture(t)
	err := db.Write(t.Context(), func(tx *sql.Tx) error {
		for i := 0; i < 260; i++ {
			if _, err := tx.Exec("INSERT INTO ext_lineage_node VALUES(?,'7','kb','chunk',?,'2026-11-05T00:00:00Z')", fmt.Sprint(i), fmt.Sprint(i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := r.Graph(t.Context(), fixtureScope, "provenance", "")
	if err != nil || len(graph.Nodes) != 256 || !graph.Truncated {
		t.Fatalf("node limit not reported: %d %v %v", len(graph.Nodes), graph.Truncated, err)
	}
}
