package attachmentworkbench

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestTasksReportNameAndMappingWorkOnlyForCurrentAttachments(t *testing.T) {
	const (
		kb      = "11111111-1111-4111-8111-111111111111"
		parent  = "22222222-2222-4222-8222-222222222222"
		childA  = "33333333-3333-4333-8333-333333333333"
		childB  = "44444444-4444-4444-8444-444444444444"
		parentH = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	row := func(id, title, file, importID, sha string) map[string]any {
		return map[string]any{
			"id": id, "tenant_id": 7, "knowledge_base_id": kb, "title": title, "file_name": file,
			"file_hash": strings.Repeat("b", 32), "parse_status": "completed", "enable_status": "enabled",
			"metadata": map[string]string{"is_attachment": "true", "parent_document_id": parent,
				"attachment_import_id": importID, "attachment_sha256": sha},
		}
	}
	parentRow := map[string]any{"id": parent, "tenant_id": 7, "knowledge_base_id": kb, "title": "主件",
		"file_name": "主件.docx", "file_hash": parentH, "parse_status": "completed", "enable_status": "enabled", "metadata": map[string]string{}}
	rows := []map[string]any{
		row(childA, "annex-01.docx", "annex-01.docx", "imp-a", strings.Repeat("d", 64)),
		row(childB, "annex-02.docx", "annex-02.docx", "imp-b", strings.Repeat("e", 64)),
	}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}) }
		switch r.URL.Path {
		case "/api/v1/knowledge-bases/" + kb + "/knowledge":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows), "page": 1, "page_size": 100})
		case "/api/v1/knowledge/batch":
			out := []map[string]any{}
			for _, id := range r.URL.Query()["ids"] {
				if id == parent {
					out = append(out, parentRow)
				}
			}
			send(out)
		default:
			http.NotFound(w, r)
		}
	}))
	defer core.Close()
	client, err := weknora.NewClient(core.URL, "fixture", 7)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	db, err := store.OpenDurable(ctx, filepath.Join(t.TempDir(), "extension.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		insert := `INSERT INTO ext_attachment_links(import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,
			attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at,evidence_kind)
			VALUES(?,?,?,?,?,?,?,?,1,?, 'upload_receipt')`
		for _, item := range []struct{ importID, id, sha, hash string }{
			{"imp-a", childA, strings.Repeat("d", 64), strings.Repeat("b", 32)},
			{"imp-b", childB, strings.Repeat("e", 64), strings.Repeat("b", 32)},
		} {
			if _, err := tx.ExecContext(ctx, insert, item.importID, "7", kb, parent, parentH, item.sha, item.hash, item.id, "2026-01-01T00:00:00Z"); err != nil {
				return err
			}
		}
		semantic := `INSERT INTO ext_attachment_semantics(attachment_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,
			original_filename,display_name,attachment_sequence,reference_title,reference_location,mapping_status,mapping_confidence,index_policy,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
		if _, err := tx.ExecContext(ctx, semantic, childA, 7, kb, parent, parentH, "annex-01.docx", "中国邮政储蓄银行上门人员备案表", 1, "附件1：人员备案表", "基本规定", "candidate", 0.9, "default", "2026-01-01T00:00:00Z"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, semantic, childB, 7, kb, parent, parentH, "annex-02.docx", "中国邮政储蓄银行上门人员备案表", 2, "", "", "confirmed", 1, "default", "2026-01-01T00:00:00Z"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := New(db, client, nil)
	tasks, err := s.Tasks(ctx, Scope{Tenant: 7, KB: kb})
	if err != nil {
		t.Fatal(err)
	}
	if tasks.Total != 2 || len(tasks.Documents) != 1 || tasks.Documents[0].Count != 2 {
		t.Fatalf("unexpected task list: %+v", tasks)
	}
	for _, item := range tasks.Documents[0].Items {
		joined := strings.Join(item.Reasons, ",")
		if !strings.Contains(joined, "name_review") || !strings.Contains(joined, "duplicate_name") {
			t.Fatalf("attachment %s reasons=%v", item.ID, item.Reasons)
		}
	}
	if got := strings.Join(tasks.Documents[0].Items[0].Reasons, ","); !strings.Contains(got, "mapping_review") {
		t.Fatalf("candidate mapping not reported: %v", tasks.Documents[0].Items[0].Reasons)
	}
}
