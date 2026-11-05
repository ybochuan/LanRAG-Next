package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/retrieval"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStreamableDiscoveryScopeAndLifecycle(t *testing.T) {
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(t.TempDir(), "ext.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	docs := []any{map[string]any{"id": "parent", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "parent.pdf"}, map[string]any{"id": "child", "tenant_id": 7, "knowledge_base_id": "kb-a", "file_name": "form.pdf", "metadata": map[string]string{"is_attachment": "true", "parent_document_id": "parent"}}}
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := docs
		if r.URL.Path == "/api/v1/knowledge/batch" {
			rows = []any{}
			for _, id := range r.URL.Query()["ids"] {
				for _, d := range docs {
					if d.(map[string]any)["id"] == id {
						rows = append(rows, d)
					}
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": rows, "total": len(rows), "page": 1, "page_size": 100})
	}))
	defer native.Close()
	for _, id := range []string{"parent", "child"} {
		_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: id, TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id, ToState: lifecycle.Active, Reason: "test", OccurredAt: time.Now()})
		if e != nil {
			t.Fatal(e)
		}
	}
	c, _ := weknora.NewClient(native.URL, "test", 7)
	p, _ := retrieval.NewPolicy(db, 7, []string{"kb-a"})
	p.WithAttachments(c)
	s := &Server{DB: db, Client: c, Policy: p, Key: strings.Repeat("k", 32), Tenant: 7, KBs: []string{"kb-a"}}
	endpoint := httptest.NewServer(s)
	defer endpoint.Close()
	call := func(body string, key bool) *httptest.ResponseRecorder {
		r, err := http.NewRequest("POST", endpoint.URL+"/mcp", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if key {
			r.Header.Set("Authorization", "Bearer "+s.Key)
		}
		w := httptest.NewRecorder()
		response, err := endpoint.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		io.Copy(w.Body, response.Body)
		return w
	}
	if !strings.Contains(call(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`, true).Body.String(), `"protocolVersion":"2025-03-26"`) {
		t.Fatal("initialization failed")
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if call(list, false).Code != 401 || !strings.Contains(call(list, true).Body.String(), "query_related_docs") {
		t.Fatal("auth/discovery failed")
	}
	request := func(id string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query_related_docs","arguments":{"document_id":%q,"_native_scope":{"tenant_id":7,"knowledge_base_ids":["kb-a"],"knowledge_ids":["parent"]}}}}`, id)
	}
	if !strings.Contains(call(request("parent"), true).Body.String(), "/api/v1/knowledge/child/download") {
		t.Fatal("attachment link missing")
	}
	if !strings.Contains(call(request("child"), true).Body.String(), `"isError":true`) {
		t.Fatal("native scope bypass")
	}
	_, e = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "expired", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "parent", FromState: lifecycle.Active, ToState: lifecycle.Stale, Reason: "expired", OccurredAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(call(request("parent"), true).Body.String(), `"isError":true`) {
		t.Fatal("stale parent leaked related documents")
	}
}
