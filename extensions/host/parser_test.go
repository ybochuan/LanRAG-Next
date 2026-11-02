package host

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestParserHostConfigAndRealMountedRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer backend-fixture-key" {
			t.Error("backend credential mismatch")
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()
	env := map[string]string{"AGENTRAG_PARSER_UPSTREAM_URL": upstream.URL, "AGENTRAG_PARSER_UPSTREAM_KEY": "backend-fixture-key", "AGENTRAG_PARSER_PROXY_KEY": "core-fixture-key-not-secret", "AGENTRAG_PARSER_ARTIFACT_ROOT": filepath.Join(t.TempDir(), "artifacts"), "AGENTRAG_PARSER_SCOPES": `[{"tenant_id":7,"knowledge_base_id":"kb-a"}]`}
	cfg, err := LoadConfig(envReader(env))
	if err != nil || cfg.Parser == nil {
		t.Fatalf("parser config: %v", err)
	}
	l := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	codes := make(chan string, 20)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, l, func(code string) { codes <- code }) }()
	waitCode(t, codes, "host_probe_started")
	request, _ := http.NewRequest("GET", "http://"+l.Addr().String()+"/parser/native/v1/health", nil)
	request.Header.Set("Authorization", "Bearer core-fixture-key-not-secret")
	request.Header.Set("X-AgentRAG-Bridge-Version", "1")
	request.Header.Set("X-AgentRAG-Tenant-ID", "7")
	request.Header.Set("X-AgentRAG-KB-ID", "kb-a")
	request.Header.Set("X-AgentRAG-Doc-ID", "doc-a")
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("native route HTTP %d", response.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	delete(env, "AGENTRAG_PARSER_SCOPES")
	if _, err := LoadConfig(envReader(env)); err == nil {
		t.Fatal("partial parser config allowed")
	}
}

func TestParserConfigRejectsTrailingScopeJSON(t *testing.T) {
	env := map[string]string{"AGENTRAG_PARSER_UPSTREAM_URL": "http://127.0.0.1:8000", "AGENTRAG_PARSER_PROXY_KEY": "core-fixture-key-not-secret", "AGENTRAG_PARSER_ARTIFACT_ROOT": t.TempDir(), "AGENTRAG_PARSER_SCOPES": `[{"tenant_id":7,"knowledge_base_id":"kb-a"}] {}`}
	if _, err := LoadConfig(envReader(env)); err == nil {
		t.Fatal("trailing scope JSON accepted")
	}
}
