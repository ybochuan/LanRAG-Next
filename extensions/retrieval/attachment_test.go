package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type attachmentSourceFixture struct {
	docs []weknora.AttachmentDocument
	err  error
}

func TestAttachmentMetadataFailureNeverPermits(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	p, _ := NewPolicy(db, 7, []string{"kb-a"})
	p.WithAttachments(attachmentSourceFixture{err: errors.New("upstream unavailable")})
	r := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"child"}}}
	if _, e := p.ExcludedDocumentIDs(ctx, r); e == nil {
		t.Fatal("metadata failure permitted retrieval")
	}
	if _, e := p.AttachmentLinks(ctx, r); e == nil {
		t.Fatal("metadata failure returned links")
	}
}

func (f attachmentSourceFixture) AttachmentDocuments(context.Context, []string, []string) ([]weknora.AttachmentDocument, error) {
	return f.docs, f.err
}
func (f attachmentSourceFixture) ListAttachmentDocuments(context.Context, string) ([]weknora.AttachmentDocument, error) {
	return f.docs, f.err
}

func TestAttachmentLinksApplySemanticOrderAndHashBoundary(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, id := range []string{"parent", "child-1", "child-2"} {
		if _, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	hash := strings.Repeat("a", 32)
	source := attachmentSourceFixture{docs: []weknora.AttachmentDocument{
		{ID: "parent", KnowledgeBaseID: "kb-a", TenantID: 7, Title: "上门服务管理办法", FileHash: hash},
		{ID: "child-1", KnowledgeBaseID: "kb-a", TenantID: 7, FileName: "annex-02.docx", IsAttachment: true, ParentDocumentID: "parent"},
		{ID: "child-2", KnowledgeBaseID: "kb-a", TenantID: 7, FileName: "annex-01.docx", IsAttachment: true, ParentDocumentID: "parent"},
	}}
	if e = db.SaveAttachmentSemantics(ctx, []store.AttachmentSemantic{
		{AttachmentID: "child-1", TenantID: 7, KnowledgeBaseID: "kb-a", ParentDocumentID: "parent", ParentFileHash: hash, ParentTitle: "上门服务管理办法", OriginalFilename: "annex-02.docx", DisplayName: "合法性审查书面声明", AttachmentSequence: 2, ReferenceTitle: "合法性审查书面声明", ReferenceLocation: "第三章", MappingStatus: "confirmed", MappingConfidence: 1},
		{AttachmentID: "child-2", TenantID: 7, KnowledgeBaseID: "kb-a", ParentDocumentID: "parent", ParentFileHash: hash, ParentTitle: "上门服务管理办法", OriginalFilename: "annex-01.docx", DisplayName: "账户管理协议", AttachmentSequence: 1, ReferenceTitle: "账户管理协议", ReferenceLocation: "第四章", MappingStatus: "confirmed", MappingConfidence: 1},
	}); e != nil {
		t.Fatal(e)
	}
	p, _ := NewPolicy(db, 7, []string{"kb-a"})
	p.WithAttachments(source)
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"parent"}}}
	links, e := p.AttachmentLinks(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	items := links["parent"]
	if len(items) != 2 {
		t.Fatalf("got %d links: %+v", len(items), items)
	}
	if items[0].DisplayName != "账户管理协议" || items[1].DisplayName != "合法性审查书面声明" {
		t.Fatalf("semantic order/fields not applied: %+v", items)
	}
	if items[0].OriginalFilename != "annex-01.docx" || items[0].ParentTitle != "上门服务管理办法" || items[0].ReferenceLocation != "第四章" {
		t.Fatalf("semantic metadata missing: %+v", items[0])
	}
	source.docs[0].FileHash = strings.Repeat("b", 32)
	p.WithAttachments(source)
	links, e = p.AttachmentLinks(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if len(links["parent"]) != 2 || links["parent"][0].DisplayName != "" {
		t.Fatalf("semantic leaked across parent version: %+v", links)
	}
}
func TestAttachmentParentStateControlsRetrievalAndDownloadLinks(t *testing.T) {
	for _, state := range []string{"active", "stale", "archived", "missing", "expired-before-sweep"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			db, e := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			for _, item := range []struct{ id, state string }{{"child", "active"}, {"parent", state}} {
				if item.state == "missing" {
					continue
				}
				if item.state == "expired-before-sweep" {
					item.state = "active"
				}
				_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: item.id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: item.id, ToState: lifecycle.State(item.state), Reason: "test", OccurredAt: time.Now()})
				if e != nil {
					t.Fatal(e)
				}
			}
			if state == "expired-before-sweep" {
				if e := db.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.Exec(`INSERT INTO ext_document_annotations VALUES('7','kb-a','parent',1,NULL,'2000-01-01T00:00:00Z',NULL,'{}')`)
					return err
				}); e != nil {
					t.Fatal(e)
				}
			}
			source := attachmentSourceFixture{docs: []weknora.AttachmentDocument{{ID: "parent", KnowledgeBaseID: "kb-a", TenantID: 7}, {ID: "child", KnowledgeBaseID: "kb-a", TenantID: 7, FileName: "table.docx", IsAttachment: true, ParentDocumentID: "parent"}}}
			p, _ := NewPolicy(db, 7, []string{"kb-a"})
			p.WithAttachments(source)
			req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"child"}}}
			excluded, e := p.ExcludedDocumentIDs(ctx, req)
			if e != nil {
				t.Fatal(e)
			}
			links, e := p.AttachmentLinks(ctx, req)
			if e != nil {
				t.Fatal(e)
			}
			if state == "active" {
				if len(excluded) != 0 || len(links["child"]) != 1 || links["child"][0].DownloadURL != "/api/v1/knowledge/child/download" {
					t.Fatalf("active attachment not available: %v %v", excluded, links)
				}
			} else if len(excluded) != 1 || len(links["child"]) != 0 {
				t.Fatalf("inactive/missing parent leaked: %v %v", excluded, links)
			}
			req.KnowledgeIDs = []string{"parent"}
			links, e = p.AttachmentLinks(ctx, req)
			if e != nil {
				t.Fatal(e)
			}
			if (len(links["parent"]) == 1) != (state == "active") {
				t.Fatalf("parent hit links: %v", links)
			}
		})
	}
}

func TestManagedAttachmentRequiresBoundReceiptAndLiveParentRevision(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"parent", "child"} {
		_, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
	}
	parent := weknora.AttachmentDocument{ID: "parent", TenantID: 7, KnowledgeBaseID: "kb-a", FileHash: strings.Repeat("a", 32), ParseStatus: "completed", EnableStatus: "enabled"}
	child := weknora.AttachmentDocument{ID: "child", TenantID: 7, KnowledgeBaseID: "kb-a", FileHash: strings.Repeat("b", 32), FileName: "annex.docx", IsAttachment: true, ParentDocumentID: parent.ID}
	importID, err := db.PrepareAttachmentImport(ctx, 7, "kb-a", parent.ID, parent.FileHash, strings.Repeat("c", 64), child.FileHash)
	if err != nil {
		t.Fatal(err)
	}
	child.ImportID = importID
	if err := db.ClaimAttachmentUpload(ctx, importID); err != nil {
		t.Fatal(err)
	}
	p, _ := NewPolicy(db, 7, []string{"kb-a"})
	req := contract.BeforeRetrieveRequest{Context: contract.Context{RequestID: "r", SessionID: "s", Mode: "rag", TenantID: 7, KnowledgeBaseIDs: []string{"kb-a"}, KnowledgeIDs: []string{"child"}}}
	check := func(wantAllowed bool, docs ...weknora.AttachmentDocument) {
		t.Helper()
		p.WithAttachments(attachmentSourceFixture{docs: docs})
		excluded, err := p.ExcludedDocumentIDs(ctx, req)
		if err != nil || (len(excluded) == 0) != wantAllowed {
			t.Fatalf("want allowed=%t: exclusion=%v, err=%v", wantAllowed, excluded, err)
		}
		links, err := p.AttachmentLinks(ctx, req)
		if err != nil || (len(links["child"]) == 1) != wantAllowed {
			t.Fatalf("want allowed=%t: links=%v, err=%v", wantAllowed, links, err)
		}
	}
	check(false, parent, child)
	if err := db.BindAttachments(ctx, 7, "kb-a", parent.ID, parent.FileHash, []store.AttachmentBinding{{ImportID: importID, AttachmentID: child.ID}}); err != nil {
		t.Fatal(err)
	}
	check(true, parent, child)
	check(false, child) // Parent still appears active in the extension DB.
	changed := parent
	changed.ParseStatus = "deleting"
	check(false, changed, child)
	changed = parent
	changed.FileHash = strings.Repeat("d", 32)
	check(false, changed, child)
	child.ImportID = ""
	check(false, parent, child)
}
