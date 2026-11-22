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

func TestOverviewKeepsAttachmentMappingAfterNativeTitleRename(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES ('att-1',7,'kb','中国邮政储蓄银行上门人员备案表',NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.NewStore(s.db).Append(ctx, lifecycle.Event{EventID: "e-att", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "att-1", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.SaveAttachmentSemantics(ctx, []store.AttachmentSemantic{{
		AttachmentID: "att-1", TenantID: 7, KnowledgeBaseID: "kb", ParentDocumentID: "doc-1",
		ParentFileHash: "0123456789abcdef0123456789abcdef", OriginalFilename: "annex-01.docx",
		DisplayName: "中国邮政储蓄银行上门人员备案表", MappingStatus: "candidate", MappingConfidence: 0.92,
	}}); err != nil {
		t.Fatal(err)
	}
	s.WithNativeSQLite(path, nil)
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","page":1,"page_size":20}`, s.key)
	var out struct {
		Data struct {
			Documents []document
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || len(out.Data.Documents) != 1 {
		t.Fatalf("overview=%d %s", w.Code, w.Body.String())
	}
	d := out.Data.Documents[0]
	if d.Title != "中国邮政储蓄银行上门人员备案表" || d.NativeTitle != "中国邮政储蓄银行上门人员备案表" || d.MappingStatus != "candidate" || d.AnnexOriginal != "annex-01.docx" {
		t.Fatalf("doc=%+v", d)
	}
}

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

func TestOverviewSearchReturnsParentOfMatchingAttachment(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES ('doc-1',7,'kb','上门服务管理办法',NULL),('att-1',7,'kb','annex-01.docx',NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, event := range []lifecycle.Event{
		{EventID: "e-parent", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "doc-1", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()},
		{EventID: "e-att", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "att-1", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()},
	} {
		if _, err = lifecycle.NewStore(s.db).Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.db.SaveAttachmentSemantics(ctx, []store.AttachmentSemantic{{
		AttachmentID: "att-1", TenantID: 7, KnowledgeBaseID: "kb", ParentDocumentID: "doc-1",
		ParentTitle: "上门服务管理办法", ParentFileHash: "0123456789abcdef0123456789abcdef", OriginalFilename: "annex-01.docx",
		DisplayName: "企业电子银行人员维护申请表", MappingStatus: "confirmed", MappingConfidence: 1, AttachmentSequence: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO ext_attachment_links(import_id,tenant_id,knowledge_base_id,parent_document_id,parent_file_hash,attachment_sha256,attachment_file_hash,attachment_id,submitted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
			"import-att-1", "7", "kb", "doc-1", "0123456789abcdef0123456789abcdef", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccc", "att-1", 1, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.WithNativeSQLite(path, nil)
	w := call(s, `{"action":"overview","tenant_id":7,"knowledge_base_id":"kb","title":"人员维护","page":1,"page_size":20}`, s.key)
	var out struct {
		Data struct {
			Documents []document
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("overview=%d %s", w.Code, w.Body.String())
	}
	if len(out.Data.Documents) != 2 {
		t.Fatalf("search should return parent and attachment, got %+v", out.Data.Documents)
	}
	byID := map[string]document{}
	for _, d := range out.Data.Documents {
		byID[d.DocumentID] = d
	}
	if _, ok := byID["doc-1"]; !ok {
		t.Fatalf("parent document missing: %+v", out.Data.Documents)
	}
	child, ok := byID["att-1"]
	if !ok || child.ParentDocumentID != "doc-1" || child.ParentTitle != "上门服务管理办法" || child.AttachmentSequence != 1 {
		t.Fatalf("attachment grouping fields=%+v", child)
	}
}
