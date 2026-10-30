package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

const crashExit = 23

func TestOutboxRecoveryAfterAbruptProcessExit(t *testing.T) {
	for _, mode := range []string{"before_commit", "after_commit", "after_claim"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agentrag_ext.db")
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
			cmd.Env = append(os.Environ(), "AGENTRAG_TEST_CRASH_MODE="+mode, "AGENTRAG_TEST_CRASH_DB="+path)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != crashExit {
				t.Fatalf("helper mode=%s err=%v output=%s", mode, err, output)
			}
			ctx := context.Background()
			db, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var counts [3]int
			var oldToken string
			if err := db.View(ctx, func(tx *sql.Tx) error {
				for i, table := range []string{"ext_lifecycle_events", "ext_lifecycle_current", "ext_projection_outbox"} {
					if err := tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&counts[i]); err != nil {
						return err
					}
				}
				if mode == "after_claim" {
					return tx.QueryRow("SELECT lease_token FROM ext_projection_outbox").Scan(&oldToken)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := outbox.NewStore(db)
			now := time.Now().UTC().Add(2 * time.Minute)
			item, err := s.Claim(ctx, now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "before_commit" {
				if counts != [3]int{} || item != nil {
					t.Fatalf("uncommitted data survived: counts=%v item=%v", counts, item)
				}
				return
			}
			if counts != [3]int{1, 1, 1} || item == nil {
				t.Fatalf("committed data lost: counts=%v item=%v", counts, item)
			}
			current, err := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"})
			if err != nil || current.LastEventID != "crash-event" {
				t.Fatalf("current=%+v %v", current, err)
			}
			if oldToken != "" {
				if item.LeaseToken == oldToken {
					t.Fatal("lease token was reused")
				}
				if err := s.Complete(ctx, item.ID, oldToken, now); !errors.Is(err, outbox.ErrLeaseLost) {
					t.Fatalf("stale completion=%v", err)
				}
			}
			if err := s.Complete(ctx, item.ID, item.LeaseToken, now); err != nil {
				t.Fatal(err)
			}
			stats, err := s.Stats(ctx, outbox.Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}, now)
			if err != nil || stats.Pending != 0 || stats.Processing != 0 || stats.OldestPendingAge != 0 {
				t.Fatalf("orphan backlog=%+v %v", stats, err)
			}
		})
	}
}

// This helper only runs inside the test executable against the parent's temp DB.
// os.Exit deliberately bypasses deferred rollback/close to simulate process loss.
func TestCrashHelper(t *testing.T) {
	mode := os.Getenv("AGENTRAG_TEST_CRASH_MODE")
	if mode == "" {
		t.Skip("subprocess only")
	}
	if mode != "before_commit" && mode != "after_commit" && mode != "after_claim" {
		t.Fatal("unknown crash mode")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, os.Getenv("AGENTRAG_TEST_CRASH_DB"))
	if err != nil {
		t.Fatal(err)
	}
	event := lifecycle.Event{EventID: "crash-event", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a", ToState: "pending_review", Reason: "accepted", OccurredAt: time.Now().UTC()}
	if mode == "before_commit" {
		err := db.Write(ctx, func(tx *sql.Tx) error {
			payload, err := json.Marshal(event)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`INSERT INTO ext_lifecycle_events(event_id,tenant_id,knowledge_base_id,document_id,from_state,to_state,reason,occurred_at,source_event_id,stage,attempt,payload_json)
				VALUES('crash-event','7','kb-a','doc-a','','pending_review','accepted',?,NULL,'',0,?)`, event.OccurredAt.Format(time.RFC3339Nano), string(payload))
			if err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO ext_lifecycle_current VALUES('7','kb-a','doc-a','pending_review','crash-event')"); err != nil {
				return err
			}
			if err := outbox.EnqueueTx(ctx, tx, outbox.Item{ID: "lifecycle:crash-event", TenantID: 7, KnowledgeBaseID: "kb-a", ProjectionType: "tag_projection", AggregateType: "document", AggregateID: "doc-a", CausationType: "lifecycle_event", CausationID: "crash-event", Payload: []byte(`{"state":"pending_review"}`), CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			os.Exit(crashExit)
			return nil
		})
		t.Fatalf("failed before crash: %v", err)
	}
	if _, err := lifecycle.NewStore(db).Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	if mode == "after_claim" {
		item, err := outbox.NewStore(db).Claim(ctx, time.Now().UTC(), time.Minute)
		if err != nil || item == nil {
			t.Fatalf("claim=%v %v", item, err)
		}
	}
	os.Exit(crashExit)
}
