package cleanup

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

const (
	testTenant = 7
	testKB     = "kb-cleanup"
	testDoc    = "43db0b16-c44f-4764-9827-87d4a626b9ec"
)

func fixture(t *testing.T) (*store.DB, string, time.Time, *[]Notice) {
	t.Helper()
	ctx := context.Background()
	extPath := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.Open(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	nativePath := filepath.Join(t.TempDir(), "native.db")
	raw, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES('" + testDoc + "',7,'" + testKB + "','清理测试文档.docx','2026-11-01T00:00:00Z')"); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "e-" + testDoc, TenantID: testTenant, KnowledgeBaseID: testKB, DocumentID: testDoc, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	notices := []Notice{}
	return db, nativePath, now, &notices
}

func newService(db *store.DB, nativePath string, now time.Time, notices *[]Notice) *Service {
	return New(db, nativePath, testTenant, []string{testKB}, 30*24*time.Hour, false).
		WithNow(func() time.Time { return now }).
		WithNotifier(func(n Notice) { *notices = append(*notices, n) })
}

func TestSweepCreatesTaskAndDetectsDeletion(t *testing.T) {
	db, nativePath, now, notices := fixture(t)
	s := newService(db, nativePath, now, notices)
	ctx := context.Background()
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Tasks) != 1 {
		t.Fatalf("tasks=%+v", list)
	}
	task := list.Tasks[0]
	if task.DocumentID != testDoc || task.Title != "清理测试文档.docx" || task.Status != StatusPending {
		t.Fatalf("task=%+v", task)
	}
	if task.CleanupDueAt != now.Add(30*24*time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("due=%s", task.CleanupDueAt)
	}
	if len(*notices) != 1 || (*notices)[0].Kind != NoticeKind || (*notices)[0].SourceID == "" {
		t.Fatalf("notices=%+v", *notices)
	}
	summary, err := s.Summary(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB})
	if err != nil || summary.Pending != 1 || summary.Total != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestHoldAndConfirmCleanExtensionProjections(t *testing.T) {
	db, nativePath, now, notices := fixture(t)
	s := newService(db, nativePath, now, notices)
	ctx := context.Background()
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := s.task(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, testDoc)
	if err != nil {
		t.Fatal(err)
	}
	held, err := s.Hold(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, testDoc, 90, "审计保留", "actor:1", "op-hold", task.Revision)
	if err != nil || held.Status != StatusHeld || held.HoldUntil == "" {
		t.Fatalf("held=%+v err=%v", held, err)
	}
	// A second doc exercises the destructive path without crossing the hold.
	second := "4a1d8349-3f6c-40c8-86ab-09aa0216c163"
	raw, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("INSERT INTO knowledges VALUES(?,7,?,'第二份.docx','2026-11-02T00:00:00Z')", second, testKB); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()
	if _, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "e-" + second, TenantID: testTenant, KnowledgeBaseID: testKB, DocumentID: second, ToState: lifecycle.Active, Reason: "test", OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	task2, err := s.task(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, second)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.Confirm(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, second, "actor:1", "op-confirm", task2.Revision)
	if err != nil || confirmed.Status != StatusCleaned {
		t.Fatalf("confirmed=%+v err=%v", confirmed, err)
	}
	var current, events int
	if err := db.View(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT count(*) FROM ext_lifecycle_current WHERE document_id=?", second).Scan(&current); err != nil {
			return err
		}
		return tx.QueryRow("SELECT count(*) FROM ext_lifecycle_events WHERE document_id=?", second).Scan(&events)
	}); err != nil {
		t.Fatal(err)
	}
	if current != 0 || events != 0 {
		t.Fatalf("projection still present current=%d events=%d", current, events)
	}
	// A repeated sweep must not recreate the cleaned task.
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	task2, err = s.task(ctx, Scope{TenantID: testTenant, KnowledgeBaseID: testKB}, second)
	if err != nil || task2.Status != StatusCleaned {
		t.Fatalf("cleaned task recreated: %+v err=%v", task2, err)
	}
}
