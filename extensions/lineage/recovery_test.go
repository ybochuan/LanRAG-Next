package lineage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

func TestObservedVersionProcessCrashRecovery(t *testing.T) {
	if mode := os.Getenv("AGENTRAG_VERSION_CRASH_CHILD"); mode != "" {
		if mode == "during" {
			if e := sqlite.RegisterScalarFunction("version_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(77); return nil, nil }); e != nil {
				t.Fatal(e)
			}
		}
		db, e := store.OpenDurable(context.Background(), os.Getenv("AGENTRAG_VERSION_CRASH_DB"))
		if e != nil {
			t.Fatal(e)
		}
		if mode == "during" {
			if e = db.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec("CREATE TRIGGER version_exit_during BEFORE INSERT ON ext_lineage_audit BEGIN SELECT version_exit(); END")
				return e
			}); e != nil {
				t.Fatal(e)
			}
		}
		r := NewRegistry(db, &fixtureReader{snapshot: fixtureSnapshot(t, "first")})
		if _, e = r.Refresh(context.Background(), fixtureScope, "doc"); e != nil {
			t.Fatal(e)
		}
		os.Exit(78)
	}
	for _, mode := range []string{"during", "after"} {
		t.Run(mode, func(t *testing.T) {
			_, db, _, path := registryFixture(t)
			if e := db.Close(); e != nil {
				t.Fatal(e)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestObservedVersionProcessCrashRecovery$")
			child.Env = append(os.Environ(), "AGENTRAG_VERSION_CRASH_CHILD="+mode, "AGENTRAG_VERSION_CRASH_DB="+path)
			out, e := child.CombinedOutput()
			var exited *exec.ExitError
			want := 77
			if mode == "after" {
				want = 78
			}
			if !errors.As(e, &exited) || exited.ExitCode() != want {
				t.Fatalf("child %v output %s", e, out)
			}
			db, e = store.OpenDurable(context.Background(), path)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			check := func(expected int) {
				t.Helper()
				e := db.View(context.Background(), func(tx *sql.Tx) error {
					for _, table := range []string{"ext_lineage_node", "ext_document_version_family", "ext_document_version", "ext_source_snapshot_evidence", "ext_lineage_audit"} {
						var n int
						if e := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil {
							return e
						}
						if n != expected {
							t.Fatalf("%s=%d expected%d", table, n, expected)
						}
					}
					rows, e := tx.Query("PRAGMA foreign_key_check")
					if e != nil {
						return e
					}
					defer rows.Close()
					if rows.Next() {
						t.Fatal("broken FK")
					}
					return rows.Err()
				})
				if e != nil {
					t.Fatal(e)
				}
			}
			expected := 0
			if mode == "after" {
				expected = 1
			}
			check(expected)
			if e = db.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TRIGGER IF EXISTS version_exit_during"); return e }); e != nil {
				t.Fatal(e)
			}
			r := NewRegistry(db, &fixtureReader{snapshot: fixtureSnapshot(t, "first")})
			if _, e = r.Refresh(context.Background(), fixtureScope, "doc"); e != nil {
				t.Fatal(e)
			}
			check(1)
		})
	}
}
