package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/ingest"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
	"github.com/ybochuan/LanRAG-Next/extensions/taggov"
	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

func TestPublicAPIReconcileRestartAndProjectionRetry(t *testing.T) {
	var mu sync.Mutex
	ids := []string{"business", "finance"}
	mode := "http_failure"
	writes := 0
	catalog := []map[string]any{
		{"id": "business", "name": "business:A", "tenant_id": 7, "knowledge_base_id": "kb-a"},
		{"id": "finance", "name": "gov:domain:finance", "tenant_id": 7, "knowledge_base_id": "kb-a"},
		{"id": "active-native", "name": "gov:state:active", "tenant_id": 7, "knowledge_base_id": "kb-a"},
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-API-Key") != "test-key" || r.Header.Get("X-Tenant-ID") != "7" {
			t.Error("missing scope/auth")
		}
		var response any
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/knowledge-bases/kb-a/knowledge":
			response = map[string]any{"success": true, "total": 1, "page": 1, "page_size": 50, "data": []any{map[string]any{"id": "doc-a", "tenant_id": 7, "knowledge_base_id": "kb-a", "parse_status": "completed", "updated_at": "2026-10-30T00:00:00Z"}}}
		case "GET /api/v1/knowledge-bases/kb-a/tags":
			response = map[string]any{"success": true, "data": map[string]any{"total": 3, "page": 1, "page_size": 50, "data": catalog}}
		case "GET /api/v1/knowledge/doc-a":
			var tags any = []any{}
			if mode == "null" {
				tags = nil
			} else {
				list := make([]any, 0, len(ids))
				for _, id := range ids {
					for _, tag := range catalog {
						if tag["id"] == id {
							list = append(list, tag)
						}
					}
				}
				tags = list
			}
			response = map[string]any{"success": true, "data": map[string]any{"id": "doc-a", "tenant_id": 7, "knowledge_base_id": "kb-a", "tags": tags}}
		case "PUT /api/v1/knowledge/tags":
			writes++
			if mode == "http_failure" {
				w.WriteHeader(503)
				fmt.Fprint(w, "private failure body")
				return
			}
			var body struct {
				KB   string   `json:"kb_id"`
				Docs []string `json:"knowledge_ids"`
				Tags []string `json:"tag_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.KB != "kb-a" || len(body.Docs) != 1 || body.Docs[0] != "doc-a" {
				t.Error("wrong scoped update")
			}
			if mode != "mismatch" {
				ids = body.Tags
			}
			response = map[string]any{"success": true}
		default:
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer s.Close()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agentrag_ext.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := weknora.NewClient(s.URL, "test-key", 7)
	if err != nil {
		t.Fatal(err)
	}
	source, err := ingest.NewHTTPSource(client, 50)
	if err != nil {
		t.Fatal(err)
	}
	observer := ingest.NewObserver(db)
	r := ingest.Reconciler{Source: source, Sink: observer}
	if err := r.Sweep(ctx, ingest.Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	obs, err := ingest.NewObserver(db).Current(ctx, ingest.Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}, "doc-a")
	if err != nil || obs.ParseStatus != "completed" || obs.Origin != "reconcile" {
		t.Fatalf("restart observation=%+v %v", obs, err)
	}
	lc := lifecycle.NewStore(db)
	if _, err := lc.Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a"}); err == nil {
		t.Fatal("parse completed became governance active")
	}
	if _, err := lc.Append(ctx, lifecycle.Event{EventID: "approved-event", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-a", ToState: "active", Reason: "test approval", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	apiStore := taggov.NewHTTPStore(client)
	w, err := taggov.NewWriter(apiStore, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	p := taggov.NewProjector(db, lc, taggov.NewMapping(db, apiStore), w)
	now := time.Now().UTC().Add(time.Second)
	reports := 0
	worker := outbox.Worker{Store: outbox.NewStore(db), Handler: p.Handle, Lease: time.Minute, RetryDelay: time.Millisecond, MaxAttempts: 5, PollInterval: time.Millisecond, Now: func() time.Time { return now }, Report: func(error) { reports++ }}
	for _, nextMode := range []string{"http_failure", "mismatch", "null", "ok"} {
		mu.Lock()
		mode = nextMode
		before := writes
		mu.Unlock()
		worked, err := worker.ProcessOne(ctx)
		if !worked || (nextMode == "ok" && err != nil) || (nextMode != "ok" && err == nil) {
			t.Fatalf("mode=%s worked=%v err=%v", nextMode, worked, err)
		}
		mu.Lock()
		if nextMode == "null" && writes != before {
			t.Error("C-10 null caused a destructive replace")
		}
		mu.Unlock()
		now = now.Add(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 3 || ids[0] != "business" || ids[1] != "finance" || ids[2] != "active-native" || reports != 3 {
		t.Fatalf("tags=%v reports=%d", ids, reports)
	}
	stats, err := worker.Store.Stats(ctx, outbox.Scope{TenantID: 7, KnowledgeBaseID: "kb-a"}, now)
	if err != nil || stats.Pending != 0 || stats.Processing != 0 || stats.Dead != 0 {
		t.Fatalf("queue=%+v %v", stats, err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM ext_tag_sync_failures").Scan(&count); err != nil {
			return err
		}
		if count != 3 {
			t.Errorf("durable failures=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
