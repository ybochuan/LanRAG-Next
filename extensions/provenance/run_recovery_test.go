package provenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

func TestRunCrashRecovery(t *testing.T) {
	ctx := context.Background()
	identity := RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}
	if mode := os.Getenv("AGENTRAG_RUN_CRASH_CHILD"); mode != "" {
		if mode == "during" {
			if e := sqlite.RegisterScalarFunction("run_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(81); return nil, nil }); e != nil {
				t.Fatal(e)
			}
		}
		db, e := store.OpenDurable(ctx, os.Getenv("AGENTRAG_RUN_CRASH_DB"))
		if e != nil {
			t.Fatal(e)
		}
		if mode == "during" {
			if e = db.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.Exec("CREATE TRIGGER run_crash BEFORE INSERT ON ext_provenance_run_audit BEGIN SELECT run_exit(); END")
				return e
			}); e != nil {
				t.Fatal(e)
			}
		}
		r := NewRuns(db, inputFixture{}, &testAuthority{}, []byte(strings.Repeat("k", 32)))
		if _, e = r.Begin(ctx, identity, "begin"); e != nil {
			t.Fatal(e)
		}
		os.Exit(82)
	}
	for _, mode := range []string{"during", "after"} {
		t.Run(mode, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "runs.db")
			db, e := store.OpenDurable(ctx, file)
			if e != nil {
				t.Fatal(e)
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestRunCrashRecovery$")
			child.Env = append(os.Environ(), "AGENTRAG_RUN_CRASH_CHILD="+mode, "AGENTRAG_RUN_CRASH_DB="+file)
			out, e := child.CombinedOutput()
			var exit *exec.ExitError
			want := 81
			if mode == "after" {
				want = 82
			}
			if !errors.As(e, &exit) || exit.ExitCode() != want {
				t.Fatalf("child=%v output=%s", e, out)
			}
			db, e = store.OpenDurable(ctx, file)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			r := NewRuns(db, inputFixture{}, &testAuthority{}, []byte(strings.Repeat("k", 32)))
			e = db.View(ctx, func(tx *sql.Tx) error {
				expected := 0
				if mode == "after" {
					expected = 1
				}
				for _, table := range []string{"ext_provenance_runs", "ext_provenance_run_receipts", "ext_provenance_run_audit"} {
					var count int
					if e := tx.QueryRow("SELECT count(*) FROM " + table).Scan(&count); e != nil {
						return e
					}
					if count != expected {
						t.Fatalf("%s=%d expected=%d", table, count, expected)
					}
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if e = db.Write(ctx, func(tx *sql.Tx) error { _, e := tx.Exec("DROP TRIGGER IF EXISTS run_crash"); return e }); e != nil {
				t.Fatal(e)
			}
			h, e := r.Begin(ctx, identity, "begin")
			if e != nil {
				t.Fatal(e)
			}
			retry, e := r.Begin(ctx, identity, "begin")
			if e != nil || retry != h {
				t.Fatal("unstable reconciliation", e)
			}
			if e = db.View(ctx, func(tx *sql.Tx) error {
				var body, receipts string
				if e := tx.QueryRow("SELECT payload_json FROM ext_provenance_runs").Scan(&body); e != nil {
					return e
				}
				if e := tx.QueryRow("SELECT result_json FROM ext_provenance_run_receipts").Scan(&receipts); e != nil {
					return e
				}
				if strings.Contains(body, h.Capability) || strings.Contains(receipts, h.Capability) {
					t.Fatal("raw capability persisted")
				}
				rows, e := tx.Query("PRAGMA foreign_key_check")
				if e != nil {
					return e
				}
				defer rows.Close()
				if rows.Next() {
					t.Fatal("broken foreign key")
				}
				return rows.Err()
			}); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestRunAuditFailureRollsBackStepAndHead(t *testing.T) {
	r, h := runFixture(t)
	ctx := context.Background()
	before, e := r.Inspect(ctx, h)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.db.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec("CREATE TRIGGER run_fault BEFORE INSERT ON ext_provenance_run_audit BEGIN SELECT RAISE(ABORT,'injected'); END")
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = r.BeginStep(ctx, h, StepRequest{OperationID: "step", Kind: "model"}); e == nil {
		t.Fatal("injected failure ignored")
	}
	after, e := r.Inspect(ctx, h)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("head advanced without audit", e)
	}
	if e = r.db.View(ctx, func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRow("SELECT count(*) FROM ext_provenance_steps").Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("step survived rollback")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestRunExpiryRotationAndInputScope(t *testing.T) {
	r, h := runFixture(t)
	s, e := r.BeginStep(context.Background(), h, StepRequest{OperationID: "step", Kind: "model"})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.RecordInput(context.Background(), h, InputRequest{OperationID: "foreign", StepID: s.StepID, Evidence: InputEvidence{DocumentID: "foreign", Field: "content", ValueHash: strings.Repeat("a", 64)}}); e == nil {
		t.Fatal("scope expanded")
	}
	r.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if _, e = r.BeginStep(context.Background(), h, StepRequest{OperationID: "expired", Kind: "model"}); e == nil {
		t.Fatal("expired run mutated")
	}
	rotated := NewRuns(r.db, inputFixture{}, r.ledger.authority, []byte(strings.Repeat("z", 32)))
	if _, e = rotated.Begin(context.Background(), RunIdentity{Scope: testScope, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}, "begin"); e == nil {
		t.Fatal("rotated key reissued old capability")
	}
	// Capability is excluded from ordinary persisted snapshots by construction.
	state, _ := r.Inspect(context.Background(), h)
	body, _ := json.Marshal(state)
	if strings.Contains(string(body), h.Capability) {
		t.Fatal("snapshot exposed capability")
	}
}
