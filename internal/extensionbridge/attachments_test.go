package extensionbridge

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAttachmentResponseScopeAndNativeDownloadURL(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "foreign_url"}[bad], func(t *testing.T) {
			in := attachmentLookup{"r", "s", "rag", 7, []string{"kb-a"}, []string{"parent"}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				a := RelatedAttachment{"child", "kb-a", "parent", "table.docx", "/api/v1/knowledge/child/download"}
				if bad {
					a.DownloadURL = "https://example.invalid/file"
				}
				json.NewEncoder(w).Encode(map[string]any{"request_id": "r", "tenant_id": 7, "knowledge_base_ids": []string{"kb-a"}, "related_attachments": map[string]any{"parent": []RelatedAttachment{a}}})
			}))
			defer server.Close()
			_, e := attachmentLinksHTTP(context.Background(), server.URL, "fixture", in)
			if (e != nil) != bad {
				t.Fatalf("scope/URL check: %v", e)
			}
		})
	}
}
func TestAttachmentEnrichmentFailureClearsOldLinks(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", "")
	r := &types.SearchResult{KnowledgeID: "child", Metadata: map[string]string{"related_attachments": "old", "source": "native"}}
	EnrichAttachmentResults(context.Background(), 7, []string{"kb-a"}, []*types.SearchResult{r})
	if r.Metadata["related_attachments"] != "" || r.Metadata["source"] != "native" {
		t.Fatal("failed enrichment retained old links or removed native metadata")
	}
}
func TestAttachmentDownloadChecksCurrentPermit(t *testing.T) {
	original := EvaluateDocuments
	defer func() { EvaluateDocuments = original }()
	calls := 0
	EvaluateDocuments = func(context.Context, uint64, []string, []string) (map[string]bool, error) {
		calls++
		return map[string]bool{}, nil
	}
	k := &types.Knowledge{ID: "child", TenantID: 7, KnowledgeBaseID: "kb-a", Metadata: types.JSON(`{"is_attachment":"true","parent_document_id":"parent"}`)}
	if GuardAttachmentDownload(context.Background(), k) == nil || calls != 1 {
		t.Fatal("old attachment link bypassed permit")
	}
	k.Metadata = nil
	if GuardAttachmentDownload(context.Background(), k) != nil || calls != 1 {
		t.Fatal("ordinary download changed")
	}
}
