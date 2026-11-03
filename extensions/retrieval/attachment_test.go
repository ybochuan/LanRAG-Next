package retrieval

import (
	"context"
	"errors"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"path/filepath"
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

func TestAttachmentParentStateControlsRetrievalAndDownloadLinks(t *testing.T) {
	for _, state := range []string{"active", "stale", "archived", "missing"} {
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
				_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: item.id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: item.id, ToState: lifecycle.State(item.state), Reason: "test", OccurredAt: time.Now()})
				if e != nil {
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
