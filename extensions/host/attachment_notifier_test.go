package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ybochuan/LanRAG-Next/extensions/attachmentworkbench"
)

func TestAttachmentNotifierPostsNativeBellPayload(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notifications/internal/review-alert" || r.Header.Get("X-AgentRAG-Management-Key") == "" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := &attachmentNotifier{coreURL: server.URL, key: "0123456789012345678901234567890123456789012345678901234567890",
		client: server.Client(), sent: map[string]string{}}
	tasks := attachmentworkbench.TaskList{
		Total: 2,
		Documents: []attachmentworkbench.TaskDocument{{
			ParentID: "doc-1", ParentTitle: "上门服务管理办法", ParentFileName: "main.docx", Count: 2,
			Items: []attachmentworkbench.TaskItem{{ID: "annex-1", Reasons: []string{"name_review"}}},
		}},
	}
	if !n.deliver(context.Background(), attachmentworkbench.Scope{Tenant: 7, KB: "kb"}, tasks, "fingerprint", nil) {
		t.Fatal("delivery must succeed")
	}
	if got["kind"] != "lifecycle.attachment_due" || got["source_id"] != "fingerprint" || got["knowledge_base_id"] != "kb" || got["tenant_id"] != float64(7) {
		t.Fatalf("wrong payload: %+v", got)
	}
	if link, _ := got["link"].(string); link != "/platform/lifecycle?tab=reviews&kb=kb&document=doc-1&attachment=1" {
		t.Fatalf("wrong link: %s", link)
	}
}
