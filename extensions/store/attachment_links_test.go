package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestRetiresMissingAttachmentsAndRestoresVerifiedReuse(t *testing.T) {
	ctx := t.Context()
	db, err := OpenDurable(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, next := strings.Repeat("a", 32), strings.Repeat("b", 32)
	add := func(id, character string) AttachmentBinding {
		t.Helper()
		importID, err := db.PrepareAttachmentImport(ctx, 7, "kb", "parent", old, strings.Repeat(character, 64), strings.Repeat(character, 32))
		if err != nil {
			t.Fatal(err)
		}
		if err = db.ClaimAttachmentUpload(ctx, importID); err != nil {
			t.Fatal(err)
		}
		return AttachmentBinding{ImportID: importID, AttachmentID: id}
	}
	kept, removed := add("kept", "c"), add("removed", "d")
	if err = db.BindAttachments(ctx, 7, "kb", "parent", old, []AttachmentBinding{kept, removed}); err != nil {
		t.Fatal(err)
	}
	check := func(keptHistorical, removedHistorical bool) {
		t.Helper()
		if err := db.View(ctx, func(tx *sql.Tx) error {
			links, err := AttachmentOwnershipsTx(ctx, tx, 7, []string{"kept", "removed"})
			if err == nil && (len(links) != 2 || links["kept"].Historical != keptHistorical || links["removed"].Historical != removedHistorical) {
				t.Fatalf("manifest = %+v", links)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.ReconcileAttachmentManifest(ctx, 7, "kb", "parent", next, []AttachmentBinding{kept}, []string{"kept"}); err != nil {
		t.Fatal(err)
	}
	check(false, true)
	if err = db.ReconcileAttachmentManifest(ctx, 7, "kb", "parent", next, nil, nil); err != nil {
		t.Fatal(err)
	}
	check(true, true)
	if err = db.ReconcileAttachmentManifest(ctx, 7, "kb", "parent", old, []AttachmentBinding{kept, removed}, []string{"kept", "removed"}); err != nil {
		t.Fatal(err)
	}
	check(false, false)
}
