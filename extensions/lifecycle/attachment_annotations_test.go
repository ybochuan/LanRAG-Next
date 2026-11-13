package lifecycle

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func insertAttachmentLink(t *testing.T, db *store.DB, now time.Time) {
	t.Helper()
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO ext_attachment_links(import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			"import-1", "7", "kb-a", "parent", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccc", "child", 1, now.Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAttachmentEffectiveDatesFollowParent(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	m := NewMachine(db, func() time.Time { return now })
	cfg := NewConfiguration(db)
	parent := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "parent"}
	child := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "child"}
	from := now.Add(-time.Hour)
	until := now.Add(24 * time.Hour)
	if err := cfg.PutAnnotations(ctx, parent, Annotations{EffectiveFrom: &from, EffectiveUntil: &until}, 0); err != nil {
		t.Fatal(err)
	}
	insertAttachmentLink(t, db, now)
	childFrom := now.Add(-2 * time.Hour)
	childUntil := now.Add(2 * time.Hour)
	if err := cfg.PutAnnotations(ctx, child, Annotations{EffectiveFrom: &childFrom, EffectiveUntil: &childUntil}, 0); err != nil {
		t.Fatal(err)
	}

	snap, err := m.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Annotations.EffectiveFrom == nil || !snap.Annotations.EffectiveFrom.Equal(from) ||
		snap.Annotations.EffectiveUntil == nil || !snap.Annotations.EffectiveUntil.Equal(until) {
		t.Fatalf("child did not inherit parent dates: %+v", snap.Annotations)
	}
	if snap.AttachmentParentDocumentID != "parent" {
		t.Fatalf("child did not report inherited parent: %q", snap.AttachmentParentDocumentID)
	}
	batch, err := m.AnnotationsForDocuments(ctx, Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}, []string{"parent", "child"})
	if err != nil {
		t.Fatal(err)
	}
	if got := batch["child"]; got.EffectiveFrom == nil || !got.EffectiveFrom.Equal(from) || got.EffectiveUntil == nil || !got.EffectiveUntil.Equal(until) {
		t.Fatalf("batch child dates = %+v", got)
	}

	newFrom := now.Add(48 * time.Hour)
	newUntil := now.Add(72 * time.Hour)
	if err := cfg.PutAnnotations(ctx, parent, Annotations{EffectiveFrom: &newFrom, EffectiveUntil: &newUntil}, 1); err != nil {
		t.Fatal(err)
	}
	snap, err = m.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Annotations.EffectiveFrom == nil || !snap.Annotations.EffectiveFrom.Equal(newFrom) ||
		snap.Annotations.EffectiveUntil == nil || !snap.Annotations.EffectiveUntil.Equal(newUntil) {
		t.Fatalf("child did not follow updated parent dates: %+v", snap.Annotations)
	}

	if err := cfg.PutAnnotations(ctx, parent, Annotations{}, 2); err != nil {
		t.Fatal(err)
	}
	snap, err = m.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Annotations.EffectiveFrom != nil || snap.Annotations.EffectiveUntil != nil {
		t.Fatalf("cleared parent dates not reflected on child: %+v", snap.Annotations)
	}
	batch, err = m.AnnotationsForDocuments(ctx, Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}, []string{"child"})
	if err != nil {
		t.Fatal(err)
	}
	if got := batch["child"]; got.EffectiveFrom != nil || got.EffectiveUntil != nil {
		t.Fatalf("batch child dates not cleared with parent: %+v", got)
	}
}

func TestAttachmentExpiryFollowsParent(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	m := NewMachine(db, func() time.Time { return now })
	cfg := NewConfiguration(db)
	parent := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "parent"}
	child := Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "child"}
	from := now.Add(-time.Hour)
	until := now.Add(24 * time.Hour)
	if err := cfg.PutAnnotations(ctx, parent, Annotations{EffectiveFrom: &from, EffectiveUntil: &until}, 0); err != nil {
		t.Fatal(err)
	}
	insertAttachmentLink(t, db, now)
	mustApply(t, m, command(t, m, child, "init", "", Pending, Initialize))
	mustApply(t, m, command(t, m, child, "active", Pending, Active, ParseCompleted))

	expired := now.Add(-time.Minute)
	if err := cfg.PutAnnotations(ctx, parent, Annotations{EffectiveFrom: &from, EffectiveUntil: &expired}, 1); err != nil {
		t.Fatal(err)
	}
	if err := m.Reevaluate(ctx, child); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Current.State != Stale {
		t.Fatalf("attachment did not become stale with parent: %+v", snap.Current)
	}
}
