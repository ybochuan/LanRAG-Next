package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestHostCompletedObservationDailyCatchupAndRealPolicy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: "stale-seed", TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-stale", ToState: lifecycle.Active, Reason: "seed", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err = lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-stale"}, lifecycle.Annotations{EffectiveUntil: &past}, 0); err != nil {
		t.Fatal(err)
	}
	db.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/knowledge") {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "total": 2, "page": 1, "page_size": 50, "data": []any{map[string]any{"id": "doc-active", "tenant_id": 7, "knowledge_base_id": "kb-a", "parse_status": "completed", "updated_at": "2026-10-31T00:00:00Z"}, map[string]any{"id": "doc-stale", "tenant_id": 7, "knowledge_base_id": "kb-a", "parse_status": "completed", "updated_at": "2026-10-31T00:00:00Z"}}})
	}))
	defer api.Close()
	env := workerEnv()
	env["AGENTRAG_EXT_DB"] = path
	env["AGENTRAG_WEKNORA_URL"] = api.URL
	env["AGENTRAG_KB_IDS"] = "kb-a"
	cfg, err := LoadConfig(envReader(env))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReconcileInterval = 10 * time.Millisecond
	l := listener(t)
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(run, cfg, l, func(string) {}) }()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		body := `{"request_id":"real-policy","session_id":"s","mode":"rag","tenant_id":7,"knowledge_base_ids":["kb-a"],"knowledge_ids":["doc-active","doc-stale","unknown"]}`
		req, _ := http.NewRequest(http.MethodPost, "http://"+l.Addr().String()+"/bridge/before-retrieve", strings.NewReader(body))
		req.Header.Set(bridge.VersionHeader, bridge.Version)
		resp, err := client.Do(req)
		if err == nil {
			var decision contract.BeforeRetrieveResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&decision)
			resp.Body.Close()
			if resp.StatusCode == 200 && decodeErr == nil && reflect.DeepEqual(decision.KnowledgeIDs, []string{"doc-active"}) && reflect.DeepEqual(decision.ExcludedDocumentIDs, []string{"doc-stale", "unknown"}) && !decision.DenyAll {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("real Host policy/Observer/daily catch-up did not converge")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id, want := range map[string]lifecycle.State{"doc-active": lifecycle.Active, "doc-stale": lifecycle.Stale} {
		got, err := lifecycle.NewStore(db).Current(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: id})
		if err != nil || got.State != want {
			t.Fatalf("%s=%+v %v", id, got, err)
		}
	}
}
