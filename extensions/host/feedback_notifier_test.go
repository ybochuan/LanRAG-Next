package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFeedbackNotifierEnqueuesAndPostsWithoutBlocking(t *testing.T) {
	var calls atomic.Int32
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notifications/internal/feedback-alert" || r.Header.Get("X-AgentRAG-Management-Key") == "" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	n := newFeedbackNotifier(server.URL, "0123456789012345678901234567890123456789012345678901234567890")
	if n == nil {
		t.Fatal("notifier not built")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { n.Run(ctx, nil); close(done) }()
	start := time.Now()
	n.Signal(ctx, 7, "kb", "feedback-1")
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("Signal blocked for %v", elapsed)
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one Core call, got %d", calls.Load())
	}
	if got["feedback_id"] != "feedback-1" || got["knowledge_base_id"] != "kb" || got["tenant_id"] != float64(7) {
		t.Fatalf("wrong payload: %+v", got)
	}
	cancel()
	<-done
}

func TestFeedbackNotifierFailureNeverBlocksAndInvalidConfigIsNoop(t *testing.T) {
	if newFeedbackNotifier("", "0123456789012345678901234567890123456789012345678901234567890") != nil {
		t.Fatal("missing Core URL must disable the notifier")
	}
	var reports atomic.Int32
	n := newFeedbackNotifier("http://127.0.0.1:1", "0123456789012345678901234567890123456789012345678901234567890")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.deliver(ctx, feedbackNotice{FeedbackID: "feedback-2", TenantID: 7, KnowledgeBaseID: "kb"}, func(string) { reports.Add(1) })
	if reports.Load() != 1 {
		t.Fatal("failed delivery was not reported")
	}
}
