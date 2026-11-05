package guidance

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestPersistentBudgetConcurrentCallsAndReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: 7, KnowledgeBaseID: "kb", UserID: "u", SessionID: "session", RunID: "run"}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO ext_provenance_runs VALUES('7','kb','run',1,'open',?,'{}')", strings.Repeat("a", 64)); err != nil {
			return err
		}
		for i := 0; i < 8; i++ {
			if _, err := tx.Exec("INSERT INTO ext_feedback_guidances VALUES('7','kb',?,NULL,'seed','topic',?,0.75,'active',1,'now')", fmt.Sprint(i), strings.Repeat("x", 90)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	selectCall := func(call string) (Selection, error) {
		var out Selection
		err := db.Write(ctx, func(tx *sql.Tx) error { var err error; out, err = SelectTx(ctx, tx, scope, call); return err })
		return out, err
	}
	first, err := selectCall(strings.Repeat("0", 64))
	if err != nil || len(first.Items) != 3 || first.Content == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := selectCall(first.CallID)
	if err != nil || replay.Content != first.Content {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := selectCall(fmt.Sprintf("%064x", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.View(ctx, func(tx *sql.Tx) error {
		var total, maxUses int
		if err := tx.QueryRow("SELECT coalesce(sum(units),0) FROM ext_guidance_calls").Scan(&total); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT max(n) FROM (SELECT count(*) n FROM ext_guidance_call_items GROUP BY guidance_id)").Scan(&maxUses); err != nil {
			return err
		}
		if total > 800 || maxUses > 2 {
			t.Fatalf("total=%d maxUses=%d", total, maxUses)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := selectCall(first.CallID)
	if err != nil || replayed.Content != first.Content {
		t.Fatal("restart lost idempotent selection", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ext_feedback_guidances SET status='quarantined',revision=2 WHERE guidance_id=?", first.Items[0].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	replayed, err = selectCall(first.CallID)
	if err != nil || replayed.Content != "" {
		t.Fatal("replayed quarantined instruction", err)
	}
}
