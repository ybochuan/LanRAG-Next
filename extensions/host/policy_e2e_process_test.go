package host

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

// Opt-in process owned by m2-policy-e2e.sh, not a production service shortcut.
func TestM2PolicyHostProcess(t *testing.T) {
	ready := os.Getenv("AGENTRAG_M2_E2E_READY")
	if ready == "" {
		t.Skip("owned cross-process M2 harness only")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	err = lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-good", DocumentID: "stale"}, lifecycle.Annotations{EffectiveUntil: &past}, 0)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/knowledge") {
			w.WriteHeader(503)
			return
		}
		var docs []any
		for _, id := range []string{"active", "stale"} {
			docs = append(docs, map[string]any{"id": id, "tenant_id": 7, "knowledge_base_id": "kb-good", "parse_status": "completed", "updated_at": "2026-10-31T00:00:00Z"})
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "total": 2, "page": 1, "page_size": 50, "data": docs})
	}))
	defer api.Close()
	env := workerEnv()
	env["AGENTRAG_EXT_DB"] = path
	env["AGENTRAG_WEKNORA_URL"] = api.URL
	env["AGENTRAG_KB_IDS"] = "kb-good"
	cfg, err := LoadConfig(envReader(env))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReconcileInterval = 20 * time.Millisecond
	l, err := net.Listen("tcp4", "127.0.0.1:8090")
	if err != nil {
		t.Fatal("test port unavailable; existing owner untouched")
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, l, func(string) {}) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if ctx.Err() != nil {
			return
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8090/bridge/before-retrieve", strings.NewReader(`{"request_id":"ready","session_id":"s","mode":"rag","tenant_id":7,"knowledge_base_ids":["kb-good"],"knowledge_ids":["active","stale"]}`))
		req.Header.Set("X-AgentRAG-Bridge-Version", "1")
		client := &http.Client{Timeout: time.Second}
		response, err := client.Do(req)
		if err == nil {
			var result struct {
				IDs      []string `json:"knowledge_ids"`
				Excluded []string `json:"excluded_document_ids"`
			}
			err = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err == nil && response.StatusCode == 200 && len(result.IDs) == 1 && result.IDs[0] == "active" && len(result.Excluded) == 1 && result.Excluded[0] == "stale" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("observer/database policy did not converge")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = os.WriteFile(ready, []byte("ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
}
