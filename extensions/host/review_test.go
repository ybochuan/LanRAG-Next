package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/lifecycle"
	"github.com/ybochuan/LanRAG-Next/extensions/review"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func TestHostDeliversReviewSignalWithoutNativeTagWriteOrStateChange(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ext.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err = lifecycle.NewConfiguration(db).PutAnnotations(ctx, lifecycle.Scope{TenantID: 7, KnowledgeBaseID: "kb-a", DocumentID: "doc-review"}, lifecycle.Annotations{ReviewDue: &past}, 0); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var unexpected atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/knowledge") {
			unexpected.Add(1)
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "total": 0, "page": 1, "page_size": 50, "data": []any{}})
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
	cfg.PollInterval = 5 * time.Millisecond
	l := listener(t)
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var signals atomic.Int32
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(run, cfg, l, func(code string) {
			if code == "host_workers_started" {
				close(started)
			}
			if code == review.SignalCode {
				signals.Add(1)
			}
		})
	}()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	select {
	case <-started:
	case err := <-done:
		joined = true
		t.Fatalf("Host startup failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Host did not finish initialization")
	}
	monitor, err := store.OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err = monitor.QueryRow(`SELECT COUNT(*) FROM ext_projection_outbox WHERE projection_type='review_alert' AND status='done'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("review signal did not drain")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	err = <-done
	joined = true
	if err != nil {
		t.Fatal(err)
	}
	if signals.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("signals=%d unexpected native calls=%d", signals.Load(), unexpected.Load())
	}
	for _, table := range []string{"ext_lifecycle_events", "ext_lifecycle_current"} {
		var n int
		if err = monitor.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("review changed lifecycle %s=%d %v", table, n, err)
		}
	}
}
