package review

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"modernc.org/sqlite"
)

func TestReviewCrashAtomicityAndRestartDelivery(t *testing.T) {
	if mode := os.Getenv("AGENTRAG_REVIEW_CRASH_CHILD"); mode != "" {
		ctx := context.Background()
		if mode == "during" {
			if err := sqlite.RegisterScalarFunction("review_exit", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { os.Exit(77); return nil, nil }); err != nil {
				t.Fatal(err)
			}
		}
		db, err := store.OpenDurable(ctx, os.Getenv("AGENTRAG_REVIEW_CRASH_DB"))
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		scope := lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}
		past := now.Add(-time.Hour)
		annotate(t, db, scope, &past, 0)
		if mode == "during" {
			if err = db.Write(ctx, func(tx *sql.Tx) error {
				_, err := tx.Exec("CREATE TRIGGER exit_review BEFORE INSERT ON ext_projection_outbox BEGIN SELECT review_exit(); END")
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		svc := NewService(db, func() time.Time { return now })
		if err = svc.Check(ctx, scope); err != nil {
			t.Fatal(err)
		}
		if mode == "delivery" {
			worker := outbox.Worker{Store: outbox.NewStore(db), Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 3, PollInterval: time.Millisecond, Report: func(error) {}, Handler: svc.Handler(func(context.Context, Alert) error { fmt.Println("review_signal_before_exit"); os.Exit(79); return nil })}
			if _, err = worker.ProcessOne(ctx); err != nil {
				t.Fatal(err)
			}
			t.Fatal("expected delivery crash")
		}
		os.Exit(78)
	}
	for _, mode := range []string{"during", "after", "delivery"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "ext.db")
			child := exec.Command(os.Args[0], "-test.run=^TestReviewCrashAtomicityAndRestartDelivery$")
			child.Env = append(os.Environ(), "AGENTRAG_REVIEW_CRASH_CHILD="+mode, "AGENTRAG_REVIEW_CRASH_DB="+path)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			want := 77
			if mode == "after" {
				want = 78
			}
			if mode == "delivery" {
				want = 79
			}
			if !errors.As(err, &exited) || exited.ExitCode() != want {
				t.Fatalf("exit=%v output=%s", err, output)
			}
			db, err := store.OpenDurable(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expected := 0
			if mode != "during" {
				expected = 1
			}
			if a, q := counts(t, db); a != expected || q != expected {
				t.Fatalf("alerts=%d intents=%d expected=%d", a, q, expected)
			}
			if mode != "during" {
				recoveredNow := time.Now().Add(time.Hour)
				svc := NewService(db, func() time.Time { return recoveredNow })
				calls := 0
				if mode == "delivery" && !strings.Contains(string(output), "review_signal_before_exit") {
					t.Fatal("child did not reach notification before crash")
				}
				w := outbox.Worker{Store: outbox.NewStore(db), Lease: time.Minute, RetryDelay: time.Second, MaxAttempts: 3, PollInterval: time.Millisecond, Now: func() time.Time { return recoveredNow }, Report: func(error) {}, Handler: svc.Handler(func(context.Context, Alert) error { calls++; return nil })}
				if worked, err := w.ProcessOne(ctx); err != nil || !worked || calls != 1 {
					t.Fatalf("restart worked=%v calls=%d err=%v", worked, calls, err)
				}
			}
			if err = db.View(ctx, func(tx *sql.Tx) error {
				rows, err := tx.Query("PRAGMA foreign_key_check")
				if err != nil {
					return err
				}
				defer rows.Close()
				if rows.Next() {
					t.Fatal("review recovery FK violation")
				}
				return rows.Err()
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
