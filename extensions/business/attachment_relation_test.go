package business

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
)

func TestRelationListAttachmentInheritsParentEffectiveDates(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	scope := provenance.Scope{TenantID: 7, KnowledgeBaseID: "kb"}
	parentHash, childHash := strings.Repeat("a", 32), strings.Repeat("b", 32)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO ext_attachment_links
(import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at)
VALUES('import-1','7','kb','parent',?,?,?, 'child',1,?)`, parentHash, strings.Repeat("c", 64), childHash, now); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO ext_document_annotations
(tenant_id,knowledge_base_id,document_id,revision,effective_from,effective_until,review_due,labels_json)
VALUES('7','kb','parent',1,'2025-01-01T13:33:00Z','2026-01-01T13:33:00Z',NULL,'{}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ctx, Command{Scope: scope, Action: "relation_list", Payload: Payload{DocumentID: "child"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["edge_type"] != "attachment_of" || rows[0]["effective_from"] != "2025-01-01T13:33:00Z" || rows[0]["effective_until"] != "2026-01-01T13:33:00Z" {
		t.Fatalf("attachment relation dates = %+v", rows)
	}
}
