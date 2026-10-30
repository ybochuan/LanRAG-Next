package host

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/outbox"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func listener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func waitCode(t *testing.T, codes <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case code := <-codes:
			if code == want {
				return
			}
		case <-timer.C:
			t.Fatalf("startup event %s missing", want)
		}
	}
}

func TestProbeServesRealLoopbackWithDefaultPassThrough(t *testing.T) {
	cfg, err := LoadConfig(envReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	l := listener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	codes := make(chan string, 10)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, l, func(code string) { codes <- code }) }()
	waitCode(t, codes, "host_probe_started")
	client := &http.Client{Timeout: time.Second}
	for _, path := range []string{"/bridge/healthz", "/healthz", "/readyz", "/bridge/ingest-event", "/bridge/before-retrieve"} {
		method := "GET"
		if strings.HasPrefix(path, "/bridge/") && path != "/bridge/healthz" {
			method = "POST"
		}
		r, err := http.NewRequest(method, "http://"+l.Addr().String()+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set(bridge.VersionHeader, bridge.Version)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := 503
		if path == "/healthz" || path == "/bridge/healthz" {
			want = 200
		}
		if path == "/bridge/before-retrieve" {
			want = 400 // Empty HTTP body is malformed, even in pass-through mode.
		}
		if response.StatusCode != want || strings.Contains(string(body), "API") {
			t.Fatalf("%s status=%d body=%s", path, response.StatusCode, body)
		}
	}
	requestBody := `{"request_id":"host-pass-through","session_id":"s1","mode":"rag","tenant_id":7,"knowledge_base_ids":["kb-a"],"knowledge_ids":["doc-a"]}`
	r, err := http.NewRequest(http.MethodPost, "http://"+l.Addr().String()+"/bridge/before-retrieve", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set(bridge.VersionHeader, bridge.Version)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	var selection struct {
		KnowledgeIDs []string `json:"knowledge_ids"`
		DenyAll      bool     `json:"deny_all"`
	}
	err = json.NewDecoder(response.Body).Decode(&selection)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || selection.DenyAll || len(selection.KnowledgeIDs) != 1 || selection.KnowledgeIDs[0] != "doc-a" {
		t.Fatalf("Host default pass-through unavailable: status=%d selection=%+v error=%v", response.StatusCode, selection, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	reused, err := net.Listen("tcp4", l.Addr().String())
	if err != nil {
		t.Fatalf("listener not released: %v", err)
	}
	reused.Close()
}

func TestRuntimeStartsScopedWorkersRetriesAndClosesDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agentrag_ext.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []struct {
		tenant uint64
		kb, id string
	}{{7, "kb-a", "doc-a"}, {7, "kb-b", "doc-b"}, {8, "kb-a", "foreign"}, {7, "kb-x", "other-kb"}, {8, "kb-b", "expired"}} {
		_, err := lifecycle.NewStore(db).Append(ctx, lifecycle.Event{EventID: doc.id, TenantID: doc.tenant, KnowledgeBaseID: doc.kb, DocumentID: doc.id, ToState: "active", Reason: "fixture approval", OccurredAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if item, err := outbox.NewStore(db).ClaimScoped(ctx, time.Now().UTC(), time.Millisecond, []outbox.Scope{{TenantID: 8, KnowledgeBaseID: "kb-b"}}); err != nil || item == nil {
		t.Fatalf("expired setup=%v %v", item, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	tags := map[string][]string{"doc-a": {"kb-a-business"}, "doc-b": {"kb-b-business"}}
	var firstFailure atomic.Bool
	firstFailure.Store(true)
	var active, maximum atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-secret" || r.Header.Get("X-Tenant-ID") != "7" {
			t.Error("wrong runtime credentials/scope")
		}
		isList := strings.HasSuffix(r.URL.Path, "/knowledge")
		if !isList {
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
		}
		mu.Lock()
		defer mu.Unlock()
		var result any
		kb, doc := "kb-a", "doc-a"
		if strings.Contains(r.URL.Path, "kb-b") || strings.HasSuffix(r.URL.Path, "doc-b") {
			kb, doc = "kb-b", "doc-b"
		}
		tag := func(id, name string) map[string]any {
			return map[string]any{"id": id, "name": name, "tenant_id": 7, "knowledge_base_id": kb}
		}
		switch {
		case isList:
			if firstFailure.CompareAndSwap(true, false) {
				w.WriteHeader(503)
				fmt.Fprint(w, "private fixture body")
				return
			}
			result = map[string]any{"success": true, "total": 1, "page": 1, "page_size": 50, "data": []any{map[string]any{"id": doc, "tenant_id": 7, "knowledge_base_id": kb, "parse_status": "completed", "updated_at": "2026-10-30T00:00:00Z"}}}
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/tags"):
			result = map[string]any{"success": true, "data": map[string]any{"total": 2, "page": 1, "page_size": 50, "data": []any{tag(kb+"-business", "business:A"), tag(kb+"-active", "gov:state:active")}}}
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/knowledge/"):
			var values []any
			for _, id := range tags[doc] {
				name := "business:A"
				if strings.HasSuffix(id, "active") {
					name = "gov:state:active"
				}
				values = append(values, tag(id, name))
			}
			result = map[string]any{"success": true, "data": map[string]any{"id": doc, "tenant_id": 7, "knowledge_base_id": kb, "tags": values}}
		case r.Method == "PUT" && r.URL.Path == "/api/v1/knowledge/tags":
			var body struct {
				KB   string   `json:"kb_id"`
				Docs []string `json:"knowledge_ids"`
				Tags []string `json:"tag_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Docs) != 1 {
				t.Error("invalid runtime write")
				w.WriteHeader(400)
				return
			}
			tags[body.Docs[0]] = body.Tags
			result = map[string]any{"success": true}
		default:
			t.Errorf("unexpected runtime request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(result)
	}))
	defer api.Close()
	env := workerEnv()
	env["AGENTRAG_EXT_DB"] = path
	env["AGENTRAG_WEKNORA_URL"] = api.URL
	cfg, err := LoadConfig(envReader(env))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReconcileInterval = 10 * time.Millisecond
	cfg.PollInterval = 5 * time.Millisecond
	l := listener(t)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	codes := make(chan string, 128)
	var failureReported atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- Serve(runCtx, cfg, l, func(code string) {
			if code == "reconciliation_failed" {
				failureReported.Store(true)
			}
			codes <- code
		})
	}()
	waitCode(t, codes, "host_workers_started")
	monitor, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var observed, completed int
		if err := monitor.QueryRow("SELECT COUNT(*) FROM ext_ingest_current WHERE tenant_id='7'").Scan(&observed); err != nil {
			t.Fatal(err)
		}
		if err := monitor.QueryRow("SELECT COUNT(*) FROM ext_projection_outbox WHERE tenant_id='7' AND knowledge_base_id IN ('kb-a','kb-b') AND status='done'").Scan(&completed); err != nil {
			t.Fatal(err)
		}
		if observed == 2 && completed == 2 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("runtime did not converge: observations=%d done=%d", observed, completed)
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for len(codes) > 0 {
		code := <-codes
		if strings.Contains(code, "secret") || strings.Contains(code, "kb-") {
			t.Fatal("runtime event exposed configuration")
		}
	}
	if !failureReported.Load() || maximum.Load() != 1 {
		t.Fatalf("failure reported=%v max tag requests=%d", failureReported.Load(), maximum.Load())
	}
	var untouched int
	if err := monitor.QueryRow("SELECT COUNT(*) FROM ext_projection_outbox WHERE (tenant_id='8' AND status IN ('pending','processing')) OR (knowledge_base_id='kb-x' AND status='pending')").Scan(&untouched); err != nil || untouched != 3 {
		t.Fatalf("foreign work changed: %d %v", untouched, err)
	}
	monitor.Close()
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("DB not reopenable: %v", err)
	}
	reopened.Close()
	mu.Lock()
	defer mu.Unlock()
	for doc, ids := range tags {
		if len(ids) != 2 {
			t.Fatalf("business tags lost for %s: %v", doc, ids)
		}
	}
}

func TestStartupFailuresCloseListenerWithoutStartingWork(t *testing.T) {
	for _, scenario := range []string{"bad_config", "foreign_db", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			env := workerEnv()
			path := filepath.Join(t.TempDir(), "agentrag_ext.db")
			env["AGENTRAG_EXT_DB"] = path
			cfg, err := LoadConfig(envReader(env))
			if err != nil {
				t.Fatal(err)
			}
			var before []byte
			if scenario == "foreign_db" {
				raw, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec("CREATE TABLE knowledges(id TEXT)"); err != nil {
					t.Fatal(err)
				}
				raw.Close()
				before, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "bad_config" {
				cfg.APIKey = ""
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			l := listener(t)
			events := 0
			if err := Serve(ctx, cfg, l, func(string) { events++ }); err == nil {
				t.Fatal("bad startup succeeded")
			}
			if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("failed startup leaked listener: %v", err)
			}
			if events != 0 {
				t.Fatal("startup failure launched tasks")
			}
			if scenario == "foreign_db" {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("foreign database changed")
				}
			} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid startup created database")
			}
		})
	}
}

func TestRunPortConflictDoesNotOpenDatabase(t *testing.T) {
	occupied, err := net.Listen("tcp4", bridge.Address)
	if err != nil {
		t.Skip("8090 already in use; existing process preserved")
	}
	defer occupied.Close()
	env := workerEnv()
	path := filepath.Join(t.TempDir(), "agentrag_ext.db")
	env["AGENTRAG_EXT_DB"] = path
	cfg, err := LoadConfig(envReader(env))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), cfg, func(string) {}); err == nil {
		t.Fatal("occupied listener accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("port conflict created database")
	}
}

type invalidAddressListener struct{ net.Listener }

func (l invalidAddressListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4zero, Port: l.Listener.Addr().(*net.TCPAddr).Port}
}

func TestServeRejectsNonLoopbackAndCleansUpOnServeFailure(t *testing.T) {
	cfg, err := LoadConfig(envReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	l := listener(t)
	if err := Serve(context.Background(), cfg, invalidAddressListener{l}, func(string) {}); err == nil {
		t.Fatal("accepted nonloopback address")
	}
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatal("rejected listener leaked")
	}
	l = listener(t)
	codes := make(chan string, 10)
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), cfg, l, func(code string) { codes <- code }) }()
	waitCode(t, codes, "host_probe_started")
	l.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Serve failure was swallowed")
		}
	case <-time.After(time.Second):
		t.Fatal("Serve failure leaked owner")
	}
}
