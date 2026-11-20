package cleanup

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestConfirmCannotOverrideConcurrentHold(t *testing.T) {
	db, nativePath, now, notices := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scope := Scope{TenantID: testTenant, KnowledgeBaseID: testKB}
	s := newService(db, nativePath, now, notices)
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := s.task(ctx, scope, testDoc)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * 24 * time.Hour)
	s.WithNow(func() time.Time { return now })
	checkedNative, resume := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	confirm := New(db, nativePath, testTenant, []string{testKB}, 30*24*time.Hour, false).
		WithNow(func() time.Time {
			select {
			case <-checkedNative:
			default:
				close(checkedNative)
			}
			select {
			case <-resume:
			case <-ctx.Done():
			}
			return now
		})
	done := make(chan error, 1)
	go func() {
		_, err := confirm.Confirm(ctx, scope, testDoc, "actor:1", "confirm", task.Revision)
		done <- err
	}()
	select {
	case <-checkedNative:
	case <-ctx.Done():
		t.Fatal("confirmation did not reach the write boundary")
	}
	if _, err := s.Hold(ctx, scope, testDoc, 90, "audit", "actor:2", "hold", task.Revision); err != nil {
		t.Fatal(err)
	}
	close(resume)
	select {
	case err := <-done:
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("stale confirmation returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("confirmation did not return")
	}
	held, err := s.task(ctx, scope, testDoc)
	if err != nil || held.Status != StatusHeld || held.Revision != task.Revision+1 {
		t.Fatalf("held=%+v err=%v", held, err)
	}
	if _, err := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: testTenant, KnowledgeBaseID: testKB, DocumentID: testDoc}); err != nil {
		t.Fatalf("held document lost its lifecycle projection: %v", err)
	}
}

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
	scope := Scope{TenantID: testTenant, KnowledgeBaseID: testKB}
	due := now.Add(30 * 24 * time.Hour)
	s.WithNow(func() time.Time { return due.Add(-time.Nanosecond) })
	if _, err := s.Confirm(ctx, scope, second, "actor:1", "op-confirm", task2.Revision); !errors.Is(err, errRetentionActive) {
		t.Fatalf("early confirm error=%v", err)
	}
	unchanged, err := s.task(ctx, scope, second)
	if err != nil || unchanged != task2 {
		t.Fatalf("early confirm changed task: %+v err=%v", unchanged, err)
	}
	if _, err := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: testTenant, KnowledgeBaseID: testKB, DocumentID: second}); err != nil {
		t.Fatalf("early confirm removed lifecycle projection: %v", err)
	}
	s.WithNow(func() time.Time { return due })
	confirmed, err := s.Confirm(ctx, scope, second, "actor:1", "op-confirm", task2.Revision)
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

func TestEffectiveQueueStatusFiltersCountsAndPaginates(t *testing.T) {
	db, nativePath, now, notices := fixture(t)
	s := newService(db, nativePath, now, notices)
	ctx := context.Background()
	scope := Scope{TenantID: testTenant, KnowledgeBaseID: testKB}
	stamp := func(at time.Time) string { return at.Format(time.RFC3339Nano) }
	const retention = 30 * 24 * time.Hour
	tasks := []struct {
		id, status string
		due, first time.Time
		hold       string
	}{
		{id: "cleaned", status: StatusCleaned, due: now.Add(-96 * time.Hour)},
		{id: "held", status: StatusHeld, due: now.Add(-72 * time.Hour), hold: stamp(now.Add(time.Nanosecond))},
		{id: "hold-ended", status: StatusHeld, due: now.Add(-48 * time.Hour), hold: stamp(now)},
		{id: "failed-waiting", status: StatusFailed, due: now.Add(-24 * time.Hour), first: now.Add(-retention + time.Nanosecond)},
		{id: "failed-ready", status: StatusFailed, due: now.Add(-3 * time.Hour)},
		{id: "due-earlier", status: StatusPending, due: now.Add(-time.Hour)},
		{id: "due-now", status: StatusPending, due: now},
		{id: "pending", status: StatusPending, due: now.Add(time.Nanosecond)},
		{id: "legacy-awaiting", status: StatusAwaiting, due: now.AddDate(0, 0, 8)},
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		for _, task := range tasks {
			first := task.first
			if first.IsZero() {
				first = task.due.Add(-retention)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO ext_cleanup_tasks(tenant_id,knowledge_base_id,document_id,first_observed_at,cleanup_due_at,status,hold_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, testTenant, testKB, task.id, stamp(first), stamp(task.due), task.status, task.hold, stamp(first), stamp(first)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, scope, "", 1, 20)
	if err != nil || all.Total != 8 || len(all.Tasks) != 8 || all.Actionable != 3 {
		t.Fatalf("unfiltered list=%+v err=%v", all, err)
	}
	summary, err := s.Summary(ctx, scope)
	wantSummary := Summary{Total: 8, Pending: 2, Awaiting: 2, Held: 2, Failed: 2, DueSoon: 1, Actionable: 3}
	if err != nil || summary != wantSummary {
		t.Fatalf("summary=%+v want=%+v err=%v", summary, wantSummary, err)
	}
	for status, count := range map[string]int{StatusPending: 2, StatusAwaiting: 2, StatusHeld: 2, StatusFailed: 2, StatusCleaned: 1} {
		page, err := s.List(ctx, scope, status, 1, 20)
		if err != nil || page.Total != count || len(page.Tasks) != count || page.Counts[status] != count || page.Actionable != all.Actionable {
			t.Fatalf("filter=%s page=%+v err=%v", status, page, err)
		}
		for _, row := range page.Tasks {
			if row.EffectiveStatus != status {
				t.Fatalf("filter=%s returned row %+v", status, row)
			}
		}
	}
	for i, id := range []string{"due-earlier", "due-now"} {
		page, err := s.List(ctx, scope, StatusAwaiting, i+1, 1)
		if err != nil || page.Total != 2 || len(page.Tasks) != 1 || page.Tasks[0].DocumentID != id || page.Tasks[0].Status != StatusPending || !page.Tasks[0].CanConfirm {
			t.Fatalf("awaiting page %d: %+v err=%v", i+1, page, err)
		}
	}
	beyond, err := s.List(ctx, scope, StatusAwaiting, 3, 1)
	if err != nil || beyond.Total != 2 || len(beyond.Tasks) != 0 {
		t.Fatalf("out-of-range page=%+v err=%v", beyond, err)
	}
	expected := map[string]struct {
		status, effective, retainedUntil string
		confirm                          bool
	}{
		"held":            {StatusHeld, StatusHeld, stamp(now.Add(time.Nanosecond)), false},
		"hold-ended":      {StatusHeld, StatusHeld, stamp(now), false},
		"failed-waiting":  {StatusFailed, StatusFailed, stamp(now.Add(time.Nanosecond)), false},
		"failed-ready":    {StatusFailed, StatusFailed, stamp(now.Add(-3 * time.Hour)), true},
		"legacy-awaiting": {StatusAwaiting, StatusPending, stamp(now.AddDate(0, 0, 8)), false},
	}
	for _, task := range all.Tasks {
		if want, ok := expected[task.DocumentID]; ok {
			if task.Status != want.status || task.EffectiveStatus != want.effective || task.RetainedUntil != want.retainedUntil || task.CanConfirm != want.confirm || task.Revision != 1 {
				t.Fatalf("task=%+v want=%+v", task, want)
			}
		}
	}
	s.WithNow(func() time.Time { return now.Add(-time.Nanosecond) })
	before, err := s.Summary(ctx, scope)
	if err != nil || before.Awaiting != 1 || before.Actionable != 2 {
		t.Fatalf("before deadline=%+v err=%v", before, err)
	}
	s.WithNow(func() time.Time { return now.Add(time.Nanosecond) })
	after, err := s.Summary(ctx, scope)
	if err != nil || after.Awaiting != 3 || after.Actionable != 5 || after.Held != 2 {
		t.Fatalf("after deadline=%+v err=%v", after, err)
	}
}

func TestShortHoldCannotAdvanceCleanupAndFailureCanRetry(t *testing.T) {
	db, nativePath, now, notices := fixture(t)
	s := newService(db, nativePath, now, notices)
	ctx := context.Background()
	scope := Scope{TenantID: testTenant, KnowledgeBaseID: testKB}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := s.task(ctx, scope, testDoc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hold(ctx, scope, testDoc, 1, "audit", "actor:1", "hold", task.Revision); err != nil {
		t.Fatal(err)
	}
	due := now.Add(30 * 24 * time.Hour)
	now = now.AddDate(0, 0, 1)
	s.WithNow(func() time.Time { return now })
	s.autoConfirm = true
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, scope, "", 1, 20)
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].Status != StatusPending || list.Tasks[0].CanConfirm || list.Tasks[0].RetainedUntil != due.Format(time.RFC3339Nano) {
		t.Fatalf("released short hold: %+v err=%v", list, err)
	}
	// Older workers may have overwritten the due time on hold release, and
	// may have recorded a failure when attempting a premature confirmation.
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE ext_cleanup_tasks SET cleanup_due_at=?,status=? WHERE document_id=?", now.Format(time.RFC3339Nano), StatusFailed, testDoc)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	task, err = s.task(ctx, scope, testDoc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Confirm(ctx, scope, testDoc, "actor:1", "early-retry", task.Revision); !errors.Is(err, errRetentionActive) {
		t.Fatalf("early retry error=%v", err)
	}
	now = due
	list, err = s.List(ctx, scope, "", 1, 20)
	if err != nil || len(list.Tasks) != 1 || !list.Tasks[0].CanConfirm || list.Tasks[0].Status != StatusFailed {
		t.Fatalf("due retry: %+v err=%v", list, err)
	}
	if result, err := s.Confirm(ctx, scope, testDoc, "actor:1", "retry", task.Revision); err != nil || result.Status != StatusCleaned {
		t.Fatalf("due retry result=%+v err=%v", result, err)
	}
}
