package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestArchiveDiscoveryWarningAndAuthorizedHistoryOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(ctx, filepath.Join(dir, "ext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(dir, "native.db")
	native, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if _, err = native.Exec(`CREATE TABLE knowledges (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, parse_status TEXT, deleted_at DATETIME);
CREATE TABLE chunks (id TEXT, tenant_id INTEGER, knowledge_base_id TEXT, knowledge_id TEXT, content TEXT, chunk_index INTEGER, chunk_type TEXT, is_enabled INTEGER, deleted_at DATETIME);`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id, kb  string
		tenant  uint64
		state   lifecycle.State
		deleted bool
	}{
		{"stale-doc", "kb", 7, lifecycle.Stale, false}, {"archived-doc", "kb", 7, "archived", false}, {"active-doc", "kb", 7, lifecycle.Active, false}, {"other-kb", "other", 7, lifecycle.Stale, false}, {"foreign", "kb", 8, lifecycle.Stale, false}, {"deleted-doc", "kb", 7, lifecycle.Stale, true},
	} {
		ls := lifecycle.NewStore(db)
		e := lifecycle.Event{EventID: v.id + "-active", TenantID: v.tenant, KnowledgeBaseID: v.kb, DocumentID: v.id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()}
		if _, err = ls.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
		if v.state != lifecycle.Active {
			e.EventID, e.FromState, e.ToState = v.id+"-changed", lifecycle.Active, v.state
			if _, err = ls.Append(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = native.Exec(`INSERT INTO knowledges VALUES (?,?,?,?,'completed',CASE WHEN ? THEN '2026-01-01' END)`, v.id, v.tenant, v.kb, v.id, v.deleted); err != nil {
			t.Fatal(err)
		}
		if _, err = native.Exec(`INSERT INTO chunks VALUES (?,?,?,?,?,1,'text',1,NULL)`, v.id, v.tenant, v.kb, v.id, "历史条款 "+v.id); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{DB: db, Key: strings.Repeat("k", 32), Tenant: 7, KBs: []string{"kb", "other"}, NativeDBPath: path}
	call := func(method string, params any) []byte {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.Key)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		return w.Body.Bytes()
	}
	if !strings.Contains(string(call("tools/list", nil)), "search_archived_documents") {
		t.Fatal("archive tool not advertised")
	}
	search := func(scope nativeScope) (string, bool) {
		var out struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
		}
		if err := json.Unmarshal(call("tools/call", map[string]any{"name": "search_archived_documents", "arguments": map[string]any{"query": "历史条款", "_native_scope": scope}}), &out); err != nil || len(out.Result.Content) != 1 {
			t.Fatal("invalid MCP tool result", err)
		}
		return out.Result.Content[0].Text, out.Result.IsError
	}
	text, failed := search(nativeScope{Tenant: 7, KBs: []string{"kb"}, WholeKBs: []string{"kb"}})
	if failed || !strings.HasPrefix(text, "【系统提示：以下内容来自历史归档文档，已失效，请在回答时提醒用户】") || !strings.Contains(text, "stale-doc") || !strings.Contains(text, "archived-doc") {
		t.Fatalf("missing warning or results: %s", text)
	}
	for _, id := range []string{"active-doc", "other-kb", "foreign", "deleted-doc"} {
		if strings.Contains(text, id) {
			t.Fatalf("unexpected hit %s", id)
		}
	}
	text, failed = search(nativeScope{Tenant: 7, KBs: []string{"kb"}, IDs: []string{"stale-doc"}})
	if failed || !strings.Contains(text, "stale-doc") || strings.Contains(text, "archived-doc") {
		t.Fatal("document scope not respected")
	}
	if _, failed = search(nativeScope{}); !failed {
		t.Fatal("missing scope allowed")
	}
}
