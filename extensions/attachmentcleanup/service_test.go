package attachmentcleanup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestOriginalReclaimWaitsForTombstoneAndRetriesAcrossRestart(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	extPath, nativePath := filepath.Join(dir, "ext.db"), filepath.Join(dir, "native.db")
	original := filepath.Join(dir, "annex.docx")
	if err := os.WriteFile(original, []byte("test attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenDurable(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	native, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	statements := []string{
		"CREATE TABLE knowledges(id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, file_hash TEXT, metadata TEXT, parse_status TEXT, deleted_at TEXT)",
		"INSERT INTO knowledges VALUES('annex',7,'kb','file-revision','{}','completed',NULL)",
	}
	for _, statement := range statements {
		if _, err := native.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now, attempts := time.Now().UTC(), 0
	reclaimer := func(_ context.Context, kb, id, hash string) (string, error) {
		if kb != "kb" || id != "annex" || hash != "file-revision" {
			t.Fatal("unexpected scoped file reclaim")
		}
		attempts++
		if attempts == 1 {
			return "", errors.New("fixture storage unavailable")
		}
		if err := os.Remove(original); err != nil {
			return "", err
		}
		return "removed", nil
	}
	makeWorker := func() *Service {
		s := New(db, nativePath, 7, []string{"kb"}, nil, time.Second).WithReclaimer(reclaimer)
		s.now = func() time.Time { return now }
		return s
	}
	s := makeWorker()
	if err := s.Prepare(ctx, 7, []Target{{DocumentID: "annex", KnowledgeBaseID: "kb", FileHash: "file-revision"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx); err != nil || attempts != 0 {
		t.Fatalf("reclaimed a live object: attempts=%d error=%v", attempts, err)
	}
	if _, err := native.Exec("UPDATE knowledges SET deleted_at='2026-11-19' WHERE id='annex'"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := s.Sweep(ctx); err == nil || attempts != 1 {
		t.Fatalf("missing failed recovery attempt: attempts=%d error=%v", attempts, err)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("unavailable storage lost its original")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := makeWorker().Sweep(ctx); err != nil || attempts != 2 {
		t.Fatalf("restart did not complete reclaim: attempts=%d error=%v", attempts, err)
	}
	if _, err := os.Stat(original); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("test original was not removed")
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var state string
		if err := tx.QueryRow("SELECT status FROM ext_attachment_reclaims WHERE document_id='annex'").Scan(&state); err != nil {
			return err
		}
		if state != "removed" {
			t.Fatalf("unexpected final reclaim state %q", state)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupWaitsForNativeDeletionAndResumesAfterRestart(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	extPath, nativePath := filepath.Join(dir, "ext.db"), filepath.Join(dir, "native.db")
	db, err := store.OpenDurable(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	native, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := native.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE knowledges(id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT,
		file_hash TEXT, metadata TEXT, parse_status TEXT, deleted_at TEXT)`)
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	add := func(id, receipt, hash string) {
		t.Helper()
		metadata := "{}"
		if id != "parent" {
			metadata = fmt.Sprintf(`{"is_attachment":"true","parent_document_id":"parent","attachment_import_id":%q}`, receipt)
		}
		exec("INSERT INTO knowledges VALUES(? ,7,'kb',?,?,'completed',NULL)", id, hash, metadata)
	}
	add("parent", "", parentHash)
	add("legacy", "", childHash)
	bind := func(childID, digest string) string {
		t.Helper()
		importID, err := db.PrepareAttachmentImport(ctx, 7, "kb", "parent", parentHash, digest, childHash)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.ClaimAttachmentUpload(ctx, importID); err != nil {
			t.Fatal(err)
		}
		if err = db.BindAttachments(ctx, 7, "kb", "parent", parentHash, []store.AttachmentBinding{{ImportID: importID, AttachmentID: childID}}); err != nil {
			t.Fatal(err)
		}
		return importID
	}
	childReceipt := bind("child", strings.Repeat("c", 64))
	add("child", childReceipt, childHash)
	bind("conflict", strings.Repeat("d", 64))
	add("conflict", "unrelated-receipt", childHash)

	now := time.Now().UTC()
	requests := 0
	deleteChild := func(_ context.Context, kb, parent, id string) error {
		if kb != "kb" || parent != "parent" || id != "child" {
			t.Fatalf("unexpected native delete %s/%s", kb, id)
		}
		requests++
		exec("UPDATE knowledges SET parse_status='deleting' WHERE id='child'")
		return errors.New("native response lost")
	}
	newService := func() *Service {
		s := New(db, nativePath, 7, []string{"kb"}, deleteChild, time.Second)
		s.now = func() time.Time { return now }
		return s
	}
	s := newService()
	target := Target{DocumentID: "parent", KnowledgeBaseID: "kb", FileHash: parentHash}
	if err := s.Prepare(ctx, 7, []Target{target}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PrepareAttachmentImport(ctx, 7, "kb", "parent", parentHash, strings.Repeat("e", 64), childHash); err == nil {
		t.Fatal("new attachment could start after delete intent")
	}
	if err := s.Sweep(ctx); err != nil || requests != 0 {
		t.Fatalf("live parent triggered cleanup: %d, %v", requests, err)
	}

	exec("UPDATE knowledges SET deleted_at='2026-11-08T00:00:00Z' WHERE id='parent'")
	now = now.Add(2 * time.Second)
	if err := s.Sweep(ctx); err == nil || requests != 1 {
		t.Fatalf("cleanup did not submit the verified child: %d, %v", requests, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, extPath)
	if err != nil {
		t.Fatal(err)
	}
	s = newService()
	if err := s.Prepare(ctx, 7, []Target{{DocumentID: "child", KnowledgeBaseID: "kb", ParentDocumentID: "parent", ImportID: childReceipt, FileHash: parentHash, IsAttachment: true}}); err == nil {
		t.Fatal("queued cleanup allowed a changed attachment to be deleted")
	}
	now = now.Add(2 * time.Second)
	_ = s.Sweep(ctx)
	if requests != 1 {
		t.Fatal("duplicate delete while native deletion was already in progress")
	}
	exec("UPDATE knowledges SET deleted_at='2026-11-08T00:00:01Z' WHERE id='child'")
	now = now.Add(2 * time.Second)
	_ = s.Sweep(ctx)
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		err := tx.QueryRow(`SELECT COUNT(*) FROM ext_attachment_links WHERE attachment_id='child' AND removed_at IS NOT NULL`).Scan(&count)
		if err == nil && count != 1 {
			t.Fatal("confirmed native deletion did not complete the durable intent")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareDeletionFencesUploadsAndRollsBackWholeBatch(t *testing.T) {
	ctx := t.Context()
	db, err := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	parentHash, childHash, digest := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 64)
	ready, err := db.PrepareAttachmentImport(ctx, 7, "kb", "first", parentHash, digest, childHash)
	if err != nil {
		t.Fatal(err)
	}
	inflight, err := db.PrepareAttachmentImport(ctx, 7, "kb", "second", parentHash, digest, childHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimAttachmentUpload(ctx, inflight); err != nil {
		t.Fatal(err)
	}
	targets := []Target{{DocumentID: "first", KnowledgeBaseID: "kb"}, {DocumentID: "second", KnowledgeBaseID: "kb"}}
	s := New(db, "", 7, []string{"kb"}, nil, time.Second)
	if err := s.Prepare(ctx, 7, targets); !errors.Is(err, ErrConflict) {
		t.Fatalf("unresolved upload did not block native delete: %v", err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_attachment_delete_intents").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("a partial batch intent committed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAttachments(ctx, 7, "kb", "second", parentHash, []store.AttachmentBinding{{ImportID: inflight, AttachmentID: "child"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prepare(ctx, 7, targets); err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimAttachmentUpload(ctx, ready); err == nil {
		t.Fatal("an existing unsubmitted upload escaped the deletion fence")
	}
}
