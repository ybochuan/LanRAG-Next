package attachmentcleanup

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestReplacementReservationCannotBeStolenOrCanceledAfterCommit(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	nativePath := filepath.Join(root, "native.db")
	native, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	_, err = native.Exec(`CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,file_hash TEXT,metadata TEXT,parse_status TEXT,deleted_at TEXT);
		INSERT INTO knowledges VALUES('parent',7,'kb','revision','{}','completed',NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenDurable(ctx, filepath.Join(root, "extension.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db, nativePath, 7, []string{"kb"}, nil, time.Second)
	target := []Target{{DocumentID: "parent", KnowledgeBaseID: "kb", FileHash: "revision"}}
	if err = s.Reserve(ctx, 7, target, "replace-first"); err != nil {
		t.Fatal(err)
	}
	if err = s.Reserve(ctx, 7, target, "replace-second"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stolen reservation = %v", err)
	}
	if err = s.CancelReservation(ctx, 7, target, "replace-second"); !errors.Is(err, ErrConflict) {
		t.Fatalf("canceled another operation = %v", err)
	}
	if _, err = db.PrepareAttachmentImport(ctx, 7, "kb", "parent", "revision", strings.Repeat("a", 64), strings.Repeat("b", 32)); err == nil {
		t.Fatal("binding bypassed reservation")
	}
	if err = s.CancelReservation(ctx, 7, target, "replace-first"); err != nil {
		t.Fatal(err)
	}
	if err = s.Validate(ctx, 7, target); err != nil {
		t.Fatal(err)
	}
	if err = s.Reserve(ctx, 7, target, "replace-next"); err != nil {
		t.Fatal(err)
	}
	if _, err = native.Exec(`UPDATE knowledges SET deleted_at='2026-11-19' WHERE id='parent'`); err != nil {
		t.Fatal(err)
	}
	if err = s.CancelReservation(ctx, 7, target, "replace-next"); !errors.Is(err, ErrConflict) {
		t.Fatalf("committed deletion was canceled = %v", err)
	}
}

func TestTransferValidationPreservesManagedRelationships(t *testing.T) {
	ctx := t.Context()
	db, err := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "extension.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db, "", 7, []string{"kb"}, nil, time.Second)
	plain := []Target{{DocumentID: "parent", KnowledgeBaseID: "kb"}}
	if err = s.ValidateTransfer(ctx, 7, plain); err != nil {
		t.Fatal(err)
	}
	if _, err = db.PrepareAttachmentImport(ctx, 7, "kb", "parent", strings.Repeat("c", 32), strings.Repeat("a", 64), strings.Repeat("b", 32)); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateTransfer(ctx, 7, plain); !errors.Is(err, ErrConflict) {
		t.Fatalf("parent move stranded its upload = %v", err)
	}
	if err = s.ValidateTransfer(ctx, 7, []Target{{DocumentID: "child", KnowledgeBaseID: "kb", IsAttachment: true}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("dedicated child escaped its parent = %v", err)
	}
}
