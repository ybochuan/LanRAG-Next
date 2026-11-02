package lifecycle

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

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

// The trigger exits the actual Apply process after its event write and before
// current/Outbox. This tests SQLite recovery, not a mocked transaction return.
func TestMachineCrashRecovery(t *testing.T) {
	if mode := os.Getenv("AGENTRAG_M2_CRASH_CHILD"); mode != "" {
		ctx := context.Background()
		if mode == "during" {
			if err := sqlite.RegisterScalarFunction("m2_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(77); return nil, nil }); err != nil {
				t.Fatal(err)
			}
		}
		db, err := store.OpenDurable(ctx, os.Getenv("AGENTRAG_M2_CRASH_DB"))
		if err != nil {
			t.Fatal(err)
		}
		if mode == "during" {
			if err = db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER exit_during BEFORE INSERT ON ext_lifecycle_current BEGIN SELECT m2_exit(); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		m := NewMachine(db, time.Now)
		s := Scope{7, "kb-a", "doc"}
		mustApply(t, m, command(t, m, s, "crash-event", "", Pending, Initialize))
		os.Exit(78)
	}
	for _, mode := range []string{"during", "after"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "extension.db")
			child := exec.Command(os.Args[0], "-test.run=^TestMachineCrashRecovery$")
			child.Env = append(os.Environ(), "AGENTRAG_M2_CRASH_CHILD="+mode, "AGENTRAG_M2_CRASH_DB="+path)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			want := 77
			if mode == "after" {
				want = 78
			}
			if !errors.As(err, &exit) || exit.ExitCode() != want {
				t.Fatalf("child=%v output=%s", err, out)
			}
			db, err := store.OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expected := 0
			if mode == "after" {
				expected = 1
			}
			if got := counts(t, db); got != [3]int{expected, expected, expected} {
				t.Fatal(got)
			}
			if err = db.View(context.Background(), func(tx *sql.Tx) error {
				for _, table := range []string{"ext_document_annotations", "ext_lifecycle_decisions"} {
					var n int
					if err := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
						return err
					}
					if n != expected {
						t.Errorf("%s=%d expected=%d", table, n, expected)
					}
				}
				rows, err := tx.Query("PRAGMA foreign_key_check")
				if err != nil {
					return err
				}
				defer rows.Close()
				if rows.Next() {
					t.Error("foreign key violation")
				}
				return rows.Err()
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
