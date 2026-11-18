package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/cleanup"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
)

func TestCleanupManagementActions(t *testing.T) {
	s := fixture(t)
	nativePath := filepath.Join(t.TempDir(), "native.db")
	raw, err := sql.Open("sqlite", nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, deleted_at DATETIME); INSERT INTO knowledges VALUES('43db0b16-c44f-4764-9827-87d4a626b9ec',7,'kb','清理测试','2026-11-01T00:00:00Z')"); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()
	if _, err := lifecycle.NewStore(s.db).Append(context.Background(), lifecycle.Event{EventID: "e-cleanup", TenantID: 7, KnowledgeBaseID: "kb", DocumentID: "43db0b16-c44f-4764-9827-87d4a626b9ec", ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	svc := cleanup.New(s.db, nativePath, 7, []string{"kb"}, 30*24*time.Hour, false)
	if err := svc.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.WithCleanup(svc)
	list := call(s, `{"action":"business_cleanup_list","tenant_id":7,"knowledge_base_id":"kb","page":1,"page_size":20,"can_write":true}`, s.key)
	var listOut struct {
		Data cleanup.ListResult
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listOut); err != nil || list.Code != 200 || listOut.Data.Total != 1 {
		t.Fatalf("list=%d %s", list.Code, list.Body.String())
	}
	hold := call(s, `{"action":"business_cleanup_hold","tenant_id":7,"knowledge_base_id":"kb","can_write":true,"actor_id":"7:user","operation_id":"op-hold","expected_revision":1,"business":{"id":"43db0b16-c44f-4764-9827-87d4a626b9ec","payload":{"hold_days":90,"reason":"审计保留"}}}`, s.key)
	var holdOut struct {
		Data cleanup.Task
	}
	if err := json.Unmarshal(hold.Body.Bytes(), &holdOut); err != nil || hold.Code != 200 || holdOut.Data.Status != cleanup.StatusHeld {
		t.Fatalf("hold=%d %s", hold.Code, hold.Body.String())
	}
}
