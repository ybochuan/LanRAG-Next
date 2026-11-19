// Package cleanup maintains a 30-day retention queue for documents that were
// soft-deleted in native storage. It only removes extension projections; the
// native document row and file are never deleted here.
package cleanup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

var (
	// ErrScope means a request cannot be bound to the configured worker scope.
	ErrScope = errors.New("cleanup scope denied")
	// ErrConflict means the task revision or native deletion evidence changed.
	ErrConflict = errors.New("cleanup task conflict")
	// ErrUnavailable means the cleanup service is not configured.
	ErrUnavailable     = errors.New("cleanup service unavailable")
	errFamilyReference = fmt.Errorf("business family still references document: %w", ErrConflict)
	errRetentionActive = fmt.Errorf("cleanup retention deadline has not been reached: %w", ErrConflict)
)

// Status values for one cleanup task.
const (
	StatusPending   = "pending_cleanup"
	StatusAwaiting  = "awaiting_confirmation"
	StatusHeld      = "held"
	StatusCleaned   = "cleaned"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// NoticeKind is the native bell notification kind for cleanup reminders.
const NoticeKind = "lifecycle.cleanup_due"

// Scope identifies one configured tenant and knowledge base.
type Scope struct {
	TenantID        uint64
	KnowledgeBaseID string
}

// Task is one deleted document's cleanup state. Title is snapshotted so the
// workbench can still name the document after the native row is gone.
type Task struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	DocumentID      string `json:"document_id"`
	Title           string `json:"title"`
	NativeDeletedAt string `json:"native_deleted_at,omitempty"`
	FirstObservedAt string `json:"first_observed_at"`
	CleanupDueAt    string `json:"cleanup_due_at"`
	Status          string `json:"status"`
	HoldUntil       string `json:"hold_until,omitempty"`
	HoldReason      string `json:"hold_reason,omitempty"`
	RetainedUntil   string `json:"retained_until,omitempty"`
	CanConfirm      bool   `json:"can_confirm"`
	LastError       string `json:"last_error,omitempty"`
	Revision        int64  `json:"revision"`
	CleanedAt       string `json:"cleaned_at,omitempty"`
}

func (s *Service) retentionDeadline(t Task) (time.Time, bool) {
	due, err := time.Parse(time.RFC3339Nano, t.CleanupDueAt)
	if err != nil {
		return time.Time{}, false
	}
	firstObserved, err := time.Parse(time.RFC3339Nano, t.FirstObservedAt)
	if err != nil {
		return time.Time{}, false
	}
	// Older sweeps overwrote the original deadline when releasing a hold.
	if earliest := firstObserved.Add(s.retention); earliest.After(due) {
		due = earliest
	}
	if t.HoldUntil != "" {
		hold, err := time.Parse(time.RFC3339Nano, t.HoldUntil)
		if err != nil {
			return time.Time{}, false
		}
		if hold.After(due) {
			due = hold
		}
	}
	return due, true
}

func (t Task) confirmableStatus() bool {
	return t.Status == StatusPending || t.Status == StatusAwaiting || t.Status == StatusFailed
}

// Summary is returned with the workbench overview for the top banner.
type Summary struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Awaiting int `json:"awaiting"`
	Held     int `json:"held"`
	Failed   int `json:"failed"`
	DueSoon  int `json:"due_soon"`
}

// ListResult is the workbench cleanup tab payload.
type ListResult struct {
	Tasks  []Task         `json:"tasks"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// Notice is delivered to the native notification bell by the Host adapter.
type Notice struct {
	TenantID        uint64
	KnowledgeBaseID string
	Title           string
	Body            string
	Link            string
	SourceID        string
	Kind            string
}

type Service struct {
	db          *store.DB
	nativePath  string
	tenant      uint64
	kbs         map[string]bool
	scopeJSON   string
	retention   time.Duration
	autoConfirm bool
	now         func() time.Time
	notify      func(Notice)
	ready       func(context.Context) error
}

// New binds cleanup to the same extension DB and native SQLite scope as Host.
func New(db *store.DB, nativePath string, tenant uint64, kbs []string, retention time.Duration, autoConfirm bool) *Service {
	allowed := make(map[string]bool, len(kbs))
	for _, kb := range kbs {
		allowed[kb] = true
	}
	encoded, _ := json.Marshal(kbs)
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &Service{db: db, nativePath: nativePath, tenant: tenant, kbs: allowed, scopeJSON: string(encoded), retention: retention, autoConfirm: autoConfirm, now: time.Now}
}

// WithNotifier installs the non-blocking native bell adapter.
func (s *Service) WithNotifier(send func(Notice)) *Service {
	s.notify = send
	return s
}

// WithReady delays the first sweep until Core can accept notifications.
func (s *Service) WithReady(ready func(context.Context) error) *Service {
	s.ready = ready
	return s
}

// WithNow is used by tests; production keeps the system clock.
func (s *Service) WithNow(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) validScope(scope Scope) bool {
	return scope.TenantID == s.tenant && s.kbs[scope.KnowledgeBaseID]
}

type nativeRow struct {
	id, kb, title, deletedAt string
}

func (s *Service) nativeRows(ctx context.Context) (map[string]nativeRow, error) {
	out := map[string]nativeRow{}
	err := s.db.ViewNative(ctx, s.nativePath, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,knowledge_base_id,COALESCE(title,''),COALESCE(deleted_at,'') FROM weknora.knowledges WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?))`, strconv.FormatUint(s.tenant, 10), s.scopeJSON)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row nativeRow
			if err := rows.Scan(&row.id, &row.kb, &row.title, &row.deletedAt); err != nil {
				return err
			}
			out[row.kb+"|"+row.id] = row
		}
		return rows.Err()
	})
	return out, err
}

func taskSelect() string {
	return `SELECT tenant_id,knowledge_base_id,document_id,title_snapshot,native_deleted_at,first_observed_at,cleanup_due_at,status,COALESCE(hold_until,''),hold_reason,last_error,revision,COALESCE(cleaned_at,'') FROM ext_cleanup_tasks`
}

func scanTask(scanner interface{ Scan(...any) error }) (Task, error) {
	var t Task
	err := scanner.Scan(&t.TenantID, &t.KnowledgeBaseID, &t.DocumentID, &t.Title, &t.NativeDeletedAt, &t.FirstObservedAt, &t.CleanupDueAt, &t.Status, &t.HoldUntil, &t.HoldReason, &t.LastError, &t.Revision, &t.CleanedAt)
	return t, err
}

func (s *Service) task(ctx context.Context, scope Scope, id string) (Task, error) {
	var out Task
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		t, err := scanTask(tx.QueryRowContext(ctx, taskSelect()+` WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id))
		out = t
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrConflict
	}
	return out, err
}

func (s *Service) insertTaskTx(ctx context.Context, tx *sql.Tx, row nativeRow, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339Nano)
	due := now.Add(s.retention).UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_cleanup_tasks(tenant_id,knowledge_base_id,document_id,title_snapshot,native_deleted_at,first_observed_at,cleanup_due_at,status,notice_detected,notice_7d,notice_1d,notice_due,last_error,attempts,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,document_id) DO UPDATE SET title_snapshot=excluded.title_snapshot,native_deleted_at=excluded.native_deleted_at,first_observed_at=excluded.first_observed_at,cleanup_due_at=excluded.cleanup_due_at,status=excluded.status,hold_until=NULL,hold_reason='',notice_detected=0,notice_7d=0,notice_1d=0,notice_due=0,last_notified_at=NULL,last_error='',attempts=0,revision=ext_cleanup_tasks.revision+1,cleaned_at=NULL,updated_at=excluded.updated_at`,
		strconv.FormatUint(s.tenant, 10), row.kb, row.id, row.title, row.deletedAt, stamp, due, StatusPending, 0, 0, 0, 0, "", 0, 1, stamp, stamp)
	return err
}

// Sweep observes native soft deletions, creates or refreshes tasks, and advances
// the reminder/confirmation state machine. It is safe to run repeatedly.
func (s *Service) Sweep(ctx context.Context) error {
	native, err := s.nativeRows(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	newCounts := map[string]int{}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, taskSelect()+` WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?))`, strconv.FormatUint(s.tenant, 10), s.scopeJSON)
		if err != nil {
			return err
		}
		defer rows.Close()
		existing := map[string]Task{}
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			existing[t.KnowledgeBaseID+"|"+t.DocumentID] = t
		}
		if err := rows.Err(); err != nil {
			return err
		}
		extKnown := map[string]bool{}
		extRows, err := tx.QueryContext(ctx, "SELECT knowledge_base_id,document_id FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?)) UNION SELECT knowledge_base_id,document_id FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?)) UNION SELECT knowledge_base_id,document_id FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?))", strconv.FormatUint(s.tenant, 10), s.scopeJSON, strconv.FormatUint(s.tenant, 10), s.scopeJSON, strconv.FormatUint(s.tenant, 10), s.scopeJSON)
		if err != nil {
			return err
		}
		for extRows.Next() {
			var kb, id string
			if err := extRows.Scan(&kb, &id); err != nil {
				extRows.Close()
				return err
			}
			extKnown[kb+"|"+id] = true
		}
		if err := extRows.Err(); err != nil {
			extRows.Close()
			return err
		}
		extRows.Close()
		for key, old := range existing {
			if !extKnown[key] && old.Status != StatusCleaned && old.Status != StatusCancelled {
				if _, err := tx.ExecContext(ctx, "UPDATE ext_cleanup_tasks SET status=?,last_error=?,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?", StatusCancelled, "no_extension_projection", now.Format(time.RFC3339Nano), strconv.FormatUint(s.tenant, 10), old.KnowledgeBaseID, old.DocumentID); err != nil {
					return err
				}
			}
		}
		for _, row := range native {
			if !s.kbs[row.kb] || !extKnown[row.kb+"|"+row.id] {
				continue
			}
			old, ok := existing[row.kb+"|"+row.id]
			if strings.TrimSpace(row.deletedAt) == "" {
				if ok && old.Status != StatusCleaned && old.Status != StatusCancelled {
					if _, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,last_error=?,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, StatusCancelled, "document_restored", now.Format(time.RFC3339Nano), strconv.FormatUint(s.tenant, 10), row.kb, row.id); err != nil {
						return err
					}
				}
				continue
			}
			if ok && old.Status == StatusCleaned && old.NativeDeletedAt == row.deletedAt {
				continue
			}
			if !ok || old.Status == StatusCleaned || old.Status == StatusCancelled {
				if err := s.insertTaskTx(ctx, tx, row, now); err != nil {
					return err
				}
				newCounts[row.kb]++
				continue
			}
			if old.Title != row.title || old.NativeDeletedAt != row.deletedAt {
				if _, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET title_snapshot=?,revision=revision+(native_deleted_at<>?),native_deleted_at=?,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, row.title, row.deletedAt, row.deletedAt, now.Format(time.RFC3339Nano), strconv.FormatUint(s.tenant, 10), row.kb, row.id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for kb, n := range newCounts {
		if err := s.sendNotice(ctx, Scope{TenantID: s.tenant, KnowledgeBaseID: kb}, "detected", n); err != nil {
			return err
		}
	}
	return s.advance(ctx)
}

func (s *Service) advance(ctx context.Context) error {
	now := s.now().UTC()
	if err := s.releaseExpiredHolds(ctx, now); err != nil {
		return err
	}
	if err := s.noticeStage(ctx, "7d", now.AddDate(0, 0, 7)); err != nil {
		return err
	}
	if err := s.noticeStage(ctx, "1d", now.AddDate(0, 0, 1)); err != nil {
		return err
	}
	return s.promoteDue(ctx, now)
}

func (s *Service) releaseExpiredHolds(ctx context.Context, now time.Time) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE ext_cleanup_tasks SET status=?,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?)) AND status=? AND COALESCE(hold_until,'')<>'' AND hold_until<=?", StatusPending, now.Format(time.RFC3339Nano), strconv.FormatUint(s.tenant, 10), s.scopeJSON, StatusHeld, now.Format(time.RFC3339Nano))
		return err
	})
}

func (s *Service) noticeStage(ctx context.Context, stage string, threshold time.Time) error {
	flag := "notice_7d"
	if stage == "1d" {
		flag = "notice_1d"
	}
	groups := map[string]int{}
	type taskID struct{ kb, id string }
	var ids []taskID
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT knowledge_base_id,document_id FROM ext_cleanup_tasks WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?)) AND status=? AND `+flag+`=0 AND cleanup_due_at<=?`, strconv.FormatUint(s.tenant, 10), s.scopeJSON, StatusPending, threshold.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var kb, id string
			if err := rows.Scan(&kb, &id); err != nil {
				return err
			}
			groups[kb]++
			ids = append(ids, taskID{kb: kb, id: id})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		stamp := s.now().UTC().Format(time.RFC3339Nano)
		for _, item := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET `+flag+`=1,last_notified_at=?,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, stamp, stamp, strconv.FormatUint(s.tenant, 10), item.kb, item.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for kb, n := range groups {
		if err := s.sendNotice(ctx, Scope{TenantID: s.tenant, KnowledgeBaseID: kb}, stage, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) promoteDue(ctx context.Context, now time.Time) error {
	var due []Task
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, taskSelect()+` WHERE tenant_id=? AND knowledge_base_id IN (SELECT value FROM json_each(?)) AND status=? AND cleanup_due_at<=?`, strconv.FormatUint(s.tenant, 10), s.scopeJSON, StatusPending, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			if deadline, ok := s.retentionDeadline(t); ok && !now.Before(deadline) {
				due = append(due, t)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	if s.autoConfirm {
		for _, t := range due {
			scope := Scope{TenantID: t.TenantID, KnowledgeBaseID: t.KnowledgeBaseID}
			if err := s.cleanTask(ctx, scope, t, "", ""); err != nil {
				_ = s.markFailed(ctx, scope, t, err)
			}
		}
		return nil
	}
	if len(due) == 0 {
		return nil
	}
	groups := map[string]int{}
	err = s.db.Write(ctx, func(tx *sql.Tx) error {
		for _, t := range due {
			res, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,revision=revision+1,notice_due=1,last_notified_at=?,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND status=? AND revision=?`, StatusAwaiting, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), strconv.FormatUint(s.tenant, 10), t.KnowledgeBaseID, t.DocumentID, StatusPending, t.Revision)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			groups[t.KnowledgeBaseID] += int(n)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for kb, n := range groups {
		if n == 0 {
			continue
		}
		if err := s.sendNotice(ctx, Scope{TenantID: s.tenant, KnowledgeBaseID: kb}, "due", n); err != nil {
			return err
		}
	}
	return nil
}

func noticeKey(scope Scope, stage string, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", scope.TenantID, scope.KnowledgeBaseID, stage, now.UTC().Format("20060102"))))
	return hex.EncodeToString(sum[:16])
}

func (s *Service) sendNotice(ctx context.Context, scope Scope, stage string, count int) error {
	now := s.now().UTC()
	key := noticeKey(scope, stage, now)
	inserted := false
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO ext_cleanup_notices(tenant_id,knowledge_base_id,notice_key,stage,item_count,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,knowledge_base_id,notice_key) DO NOTHING`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, key, stage, count, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		inserted = n == 1
		return err
	})
	if err != nil || !inserted {
		return err
	}
	if s.notify == nil {
		return nil
	}
	text := map[string]string{
		"detected": "已进入 30 天清理保留期",
		"7d":       "将在 7 天后进入清理确认",
		"1d":       "将在 1 天后进入清理确认",
		"due":      "已到清理确认时间",
	}[stage]
	if text == "" {
		text = "清理状态有更新"
	}
	s.notify(Notice{TenantID: scope.TenantID, KnowledgeBaseID: scope.KnowledgeBaseID, Title: "文档清理提醒", Body: fmt.Sprintf("本知识库有 %d 份已删除文档%s，点击查看。", count, text), Link: "/platform/documents?tab=cleanup&kb=" + url.QueryEscape(scope.KnowledgeBaseID), SourceID: key, Kind: NoticeKind})
	return nil
}

// List returns one bounded page of cleanup tasks.
func (s *Service) List(ctx context.Context, scope Scope, state string, page, pageSize int) (ListResult, error) {
	if !s.validScope(scope) || page < 1 || page > 100000 || pageSize < 1 || pageSize > 100 || state != "" && !validStatus(state) {
		return ListResult{}, ErrScope
	}
	out := ListResult{Tasks: []Task{}, Counts: map[string]int{}}
	now := s.now().UTC()
	where := "tenant_id=? AND knowledge_base_id=?"
	args := []any{strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID}
	if state != "" {
		where += " AND status=?"
		args = append(args, state)
	} else {
		where += " AND status IN (?,?,?,?)"
		args = append(args, StatusPending, StatusAwaiting, StatusHeld, StatusFailed)
	}
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM ext_cleanup_tasks WHERE "+where, args...).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, taskSelect()+" WHERE "+where+" ORDER BY cleanup_due_at,document_id LIMIT ? OFFSET ?", append(args, pageSize, (page-1)*pageSize)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if deadline, ok := s.retentionDeadline(t); ok {
				t.RetainedUntil = deadline.UTC().Format(time.RFC3339Nano)
				t.CanConfirm = t.confirmableStatus() && !now.Before(deadline)
			}
			out.Tasks = append(out.Tasks, t)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		countRows, err := tx.QueryContext(ctx, `SELECT status,count(*) FROM ext_cleanup_tasks WHERE tenant_id=? AND knowledge_base_id=? GROUP BY status`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID)
		if err != nil {
			return err
		}
		defer countRows.Close()
		for countRows.Next() {
			var status string
			var n int
			if err := countRows.Scan(&status, &n); err != nil {
				return err
			}
			out.Counts[status] = n
		}
		return countRows.Err()
	})
	return out, err
}

// Summary returns counts for the workbench banner.
func (s *Service) Summary(ctx context.Context, scope Scope) (Summary, error) {
	if !s.validScope(scope) {
		return Summary{}, ErrScope
	}
	var out Summary
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT status,count(*) FROM ext_cleanup_tasks WHERE tenant_id=? AND knowledge_base_id=? GROUP BY status`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var status string
			var n int
			if err := rows.Scan(&status, &n); err != nil {
				rows.Close()
				return err
			}
			switch status {
			case StatusPending:
				out.Pending += n
			case StatusAwaiting:
				out.Awaiting += n
			case StatusHeld:
				out.Held += n
			case StatusFailed:
				out.Failed += n
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		out.Total = out.Pending + out.Awaiting + out.Held + out.Failed
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_cleanup_tasks WHERE tenant_id=? AND knowledge_base_id=? AND status=? AND cleanup_due_at<=?`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, StatusPending, s.now().UTC().AddDate(0, 0, 7).Format(time.RFC3339Nano)).Scan(&out.DueSoon)
	})
	return out, err
}

// Hold moves a task out of automatic cleanup for the requested number of days.
func (s *Service) Hold(ctx context.Context, scope Scope, id string, days int, reason, actor, operation string, expected int64) (Task, error) {
	if !s.validScope(scope) || !weknora.ValidID(id) || days < 1 || days > 3650 || strings.TrimSpace(operation) == "" {
		return Task{}, ErrScope
	}
	now := s.now().UTC()
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,hold_until=?,hold_reason=?,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND revision=? AND status IN (?,?,?)`, StatusHeld, now.AddDate(0, 0, days).Format(time.RFC3339Nano), strings.TrimSpace(reason), now.Format(time.RFC3339Nano), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id, expected, StatusPending, StatusAwaiting, StatusFailed)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n != 1 {
			return ErrConflict
		}
		return s.auditTx(ctx, tx, scope, id, "hold", actor, map[string]any{"days": days, "reason": reason})
	})
	if err != nil {
		return Task{}, err
	}
	return s.task(ctx, scope, id)
}

// Confirm verifies the native deletion again and removes extension projections.
func (s *Service) Confirm(ctx context.Context, scope Scope, id, actor, operation string, expected int64) (Task, error) {
	if !s.validScope(scope) || !weknora.ValidID(id) || strings.TrimSpace(operation) == "" {
		return Task{}, ErrScope
	}
	t, err := s.task(ctx, scope, id)
	if err != nil {
		return Task{}, err
	}
	if t.Revision != expected || !t.confirmableStatus() {
		return Task{}, ErrConflict
	}
	if err := s.cleanTask(ctx, scope, t, actor, operation); err != nil {
		_ = s.markFailed(ctx, scope, t, err)
		return Task{}, err
	}
	return s.task(ctx, scope, id)
}

func (s *Service) cleanTask(ctx context.Context, scope Scope, task Task, actor, operation string) error {
	now := s.now().UTC()
	if deadline, ok := s.retentionDeadline(task); !ok || now.Before(deadline) {
		return errRetentionActive
	}
	id := task.DocumentID
	var deletedAt string
	err := s.db.ViewNative(ctx, s.nativePath, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COALESCE(deleted_at,'') FROM weknora.knowledges WHERE tenant_id=? AND knowledge_base_id=? AND id=?`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id).Scan(&deletedAt)
	})
	if err != nil {
		return err
	}
	if deletedAt == "" {
		if err := s.markCancelled(ctx, scope, task); err != nil {
			return err
		}
		return ErrConflict
	}
	if deletedAt != task.NativeDeletedAt {
		return ErrConflict
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,cleaned_at=?,last_error='',revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND revision=? AND status=? AND native_deleted_at=?`, StatusCleaned, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id, task.Revision, task.Status, deletedAt)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		var family int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ext_business_document_family_members WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?`, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id).Scan(&family); err != nil {
			return err
		}
		if family > 0 {
			return errFamilyReference
		}
		stmts := []string{
			"DELETE FROM ext_review_alerts WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_review_subjects WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_document_version WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_document_version_family WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_lifecycle_current WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_lifecycle_events WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_document_annotations WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
			"DELETE FROM ext_document_availability WHERE tenant_id=? AND knowledge_base_id=? AND document_id=?",
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt, strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id); err != nil {
				return err
			}
		}
		return s.auditTx(ctx, tx, scope, id, "cleaned", actor, map[string]any{"cleaned_at": now, "operation_id": operation})
	})
}

func (s *Service) markFailed(ctx context.Context, scope Scope, task Task, cause error) error {
	if cause == ErrConflict || errors.Is(cause, errRetentionActive) {
		return nil
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,last_error=?,attempts=attempts+1,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND revision=? AND status=? AND native_deleted_at=?`, StatusFailed, cause.Error(), s.now().UTC().Format(time.RFC3339Nano), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, task.DocumentID, task.Revision, task.Status, task.NativeDeletedAt)
		return err
	})
}

func (s *Service) markCancelled(ctx context.Context, scope Scope, task Task) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE ext_cleanup_tasks SET status=?,last_error=?,revision=revision+1,updated_at=? WHERE tenant_id=? AND knowledge_base_id=? AND document_id=? AND revision=? AND status=? AND native_deleted_at=?`, StatusCancelled, "document_restored", s.now().UTC().Format(time.RFC3339Nano), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, task.DocumentID, task.Revision, task.Status, task.NativeDeletedAt)
		return err
	})
}

func (s *Service) auditTx(ctx context.Context, tx *sql.Tx, scope Scope, id, action, actor string, detail map[string]any) error {
	body, _ := json.Marshal(detail)
	idPart := fmt.Sprintf("%d|%s|%s|%s|%d", scope.TenantID, scope.KnowledgeBaseID, id, action, s.now().UnixNano())
	_, err := tx.ExecContext(ctx, `INSERT INTO ext_cleanup_audit(audit_id,tenant_id,knowledge_base_id,document_id,action,actor_id,detail_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, fmt.Sprintf("%x", sha256.Sum256([]byte(idPart))), strconv.FormatUint(scope.TenantID, 10), scope.KnowledgeBaseID, id, action, actor, string(body), s.now().UTC().Format(time.RFC3339Nano))
	return err
}

func validStatus(status string) bool {
	switch status {
	case StatusPending, StatusAwaiting, StatusHeld, StatusCleaned, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Run periodically sweeps and retries failures.
func (s *Service) Run(ctx context.Context, interval time.Duration, report func(error)) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if s.ready != nil {
		for {
			if err := s.ready(ctx); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
	for ctx.Err() == nil {
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return ctx.Err()
}
