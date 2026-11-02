package lineage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

func TestSupplementProcessCrashRecovery(t *testing.T) {
	ctx := context.Background()
	if mode := os.Getenv("AGENTRAG_SUPPLEMENT_CRASH"); mode != "" {
		if mode == "during" {
			if e := sqlite.RegisterScalarFunction("supplement_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(77); return nil, nil }); e != nil {
				t.Fatal(e)
			}
		}
		db, e := store.OpenDurable(ctx, os.Getenv("AGENTRAG_SUPPLEMENT_CRASH_DB"))
		if e != nil {
			t.Fatal(e)
		}
		if mode == "during" {
			if e = db.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.Exec("CREATE TRIGGER supplement_exit_before_commit BEFORE INSERT ON ext_supplement_audit BEGIN SELECT supplement_exit(); END")
				return e
			}); e != nil {
				t.Fatal(e)
			}
		}
		reader := &multiReader{t: t, bodies: map[string]string{"child": "child", "parent": "parent"}}
		if _, e = NewSupplements(db, NewRegistry(db, reader), time.Now).Put(ctx, command("crash-retry")); e != nil {
			t.Fatal(e)
		}
		os.Exit(78)
	}
	for _, mode := range []string{"during", "after"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "crash.db")
			db, e := store.OpenDurable(ctx, path)
			if e != nil {
				t.Fatal(e)
			}
			for _, doc := range []string{"child", "parent"} {
				if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: doc, TenantID: 7, KnowledgeBaseID: "kb", DocumentID: doc, ToState: lifecycle.Active, Reason: "fixture", OccurredAt: time.Now()}); e != nil {
					t.Fatal(e)
				}
			}
			db.Close()
			child := exec.Command(os.Args[0], "-test.run=^TestSupplementProcessCrashRecovery$")
			child.Env = append(os.Environ(), "AGENTRAG_SUPPLEMENT_CRASH="+mode, "AGENTRAG_SUPPLEMENT_CRASH_DB="+path)
			out, e := child.CombinedOutput()
			var exited *exec.ExitError
			want := 77
			if mode == "after" {
				want = 78
			}
			if !errors.As(e, &exited) || exited.ExitCode() != want {
				t.Fatalf("child=%v output=%s", e, out)
			}
			db, e = store.OpenDurable(ctx, path)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			check := func(want int) {
				t.Helper()
				e := db.View(ctx, func(tx *sql.Tx) error {
					for _, table := range []string{"ext_supplement_declarations", "ext_supplement_parents", "ext_supplement_current", "ext_lineage_edge", "ext_supplement_edge_facts", "ext_supplement_audit"} {
						var n int
						if e := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil {
							return e
						}
						if n != want {
							t.Fatalf("%s=%d want=%d", table, n, want)
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
			n := 0
			if mode == "after" {
				n = 1
			}
			check(n)
			if e = db.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.Exec("DROP TRIGGER IF EXISTS supplement_exit_before_commit")
				return e
			}); e != nil {
				t.Fatal(e)
			}
			reader := &multiReader{t: t, bodies: map[string]string{"child": "child", "parent": "parent"}}
			s := NewSupplements(db, NewRegistry(db, reader), time.Now)
			first, e := s.Put(ctx, command("crash-retry"))
			if e != nil {
				t.Fatal(e)
			}
			again, e := s.Put(ctx, command("crash-retry"))
			if e != nil || first != again {
				t.Fatal("retry not idempotent", e)
			}
			check(1)
		})
	}
}
