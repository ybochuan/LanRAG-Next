package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestOverviewShowsAttachmentSemanticTitle(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES ('att-1',7,'kb','annex-01.docx',NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: "e-att", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "att-1", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.SaveAttachmentSemantics(ctx, []store.AttachmentSemantic{{
		AttachmentID: "att-1", TenantID: 7, KnowledgeBaseID: "kb", ParentDocumentID: "doc-1",
		ParentFileHash: "0123456789abcdef0123456789abcdef", OriginalFilename: "annex-01.docx",
		DisplayName: "企业电子银行人员维护申请表", MappingStatus: "confirmed", MappingConfidence: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	s.WithNativeSQLite(path, nil)
	check := func(body string, wantTitle string) {
		t.Helper()
		var out struct {
			Data struct {
				Documents []document
			}
		}
		w := call(s, body, s.key)
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || len(out.Data.Documents) != 1 {
			t.Fatalf("overview=%d %s", w.Code, w.Body.String())
		}
		d := out.Data.Documents[0]
		if d.Title != wantTitle || d.NativeTitle != "annex-01.docx" || d.MappingStatus != "confirmed" {
			t.Fatalf("doc=%+v want title=%s", d, wantTitle)
		}
	}
	check(`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page":1,"page_size":20}`, "企业电子银行人员维护申请表")
	check(`{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","title":"人员维护","page":1,"page_size":20}`, "企业电子银行人员维护申请表")
}
