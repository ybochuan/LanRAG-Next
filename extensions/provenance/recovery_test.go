package provenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

func TestManifestProcessCrashRecovery(t *testing.T) {
	if mode := os.Getenv("AGENTRAG_COPY_CRASH_CHILD"); mode != "" {
		if mode == "during" {
			if err := sqlite.RegisterScalarFunction("prov_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(77); return nil, nil }); err != nil {
				t.Fatal(err)
			}
		}
		db, err := store.OpenDurable(context.Background(), os.Getenv("AGENTRAG_COPY_CRASH_DB"))
		if err != nil {
			t.Fatal(err)
		}
		if mode == "during" {
			if err = db.Write(context.Background(), func(tx *sql.Tx) error {
				_, e := tx.Exec("CREATE TRIGGER prov_exit_during BEFORE INSERT ON ext_provenance_audit BEGIN SELECT prov_exit(); END")
				return e
			}); err != nil {
				t.Fatal(err)
			}
		}
		a := &testAuthority{copies: map[string]CopyProof{"copy": readyProof("copy", []SourceRef{{"doc", "v1"}}, nil)}}
		l := New(db, a)
		if _, err = l.Save(context.Background(), SaveRequest{Scope: testScope, CopyID: "copy", OperationID: "crash-copy", Binding: testBinding, EvidenceID: "copy"}); err != nil {
			t.Fatal(err)
		}
		os.Exit(78)
	}
	for _, mode := range []string{"during", "after"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "copy.db")
			db, err := store.OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			a := &testAuthority{sources: map[string]SourceProof{}, copies: map[string]CopyProof{}}
			seedSource(t, New(db, a), a, "doc")
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestManifestProcessCrashRecovery$")
			child.Env = append(os.Environ(), "AGENTRAG_COPY_CRASH_CHILD="+mode, "AGENTRAG_COPY_CRASH_DB="+path)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			want := 77
			if mode == "after" {
				want = 78
			}
			if !errors.As(err, &exit) || exit.ExitCode() != want {
				t.Fatalf("owned child=%v output=%s", err, out)
			}
			db, err = store.OpenDurable(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			l := New(db, a)
			counts := factCounts(t, l)
			expected := 0
			if mode == "after" {
				expected = 1
			}
			if counts[2] != expected || counts[3] != expected || counts[4] != expected || counts[6] != 1+expected || counts[7] != 1+expected {
				t.Fatal(counts)
			}
			if err = db.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TRIGGER IF EXISTS prov_exit_during"); return e }); err != nil {
				t.Fatal(err)
			}
			a.copies["copy"] = readyProof("copy", []SourceRef{{"doc", "v1"}}, nil)
			command := SaveRequest{Scope: testScope, CopyID: "copy", OperationID: "crash-copy", Binding: testBinding, EvidenceID: "copy"}
			if _, err = l.Save(context.Background(), command); err != nil {
				t.Fatal(err)
			}
			if counts = factCounts(t, l); counts[2] != 1 || counts[6] != 2 || counts[7] != 2 {
				t.Fatal(counts)
			}
			requireRead(t, l, request("copy", 1), true)
		})
	}
}
