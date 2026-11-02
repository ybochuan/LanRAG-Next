package modelsupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func portOf(t *testing.T, raw string) int {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}
	return n
}

// fakeServer stands in for llama-server. The handlers are explicit so a test can
// remove exactly one capability and assert the probe notices.
func fakeServer(t *testing.T, dim int, healthStatus int, taskStatus int) (*httptest.Server, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(healthStatus)
		if healthStatus == http.StatusOK {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, _ *http.Request) {
		if taskStatus != http.StatusOK {
			w.WriteHeader(taskStatus)
			return
		}
		embedding := make([]float64, dim)
		body, _ := json.Marshal(map[string]any{"data": []map[string]any{{"embedding": embedding}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/v1/rerank", func(w http.ResponseWriter, _ *http.Request) {
		if taskStatus != http.StatusOK {
			w.WriteHeader(taskStatus)
			return
		}
		body, _ := json.Marshal(map[string]any{"results": []map[string]any{{"index": 0, "relevance_score": 0.75}}})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		if taskStatus != http.StatusOK {
			w.WriteHeader(taskStatus)
			return
		}
		body, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "p"}}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, portOf(t, server.URL)
}

func embeddingSpec(port int) Spec {
	return Spec{ID: "embedding", Model: "Qwen3-Embedding-4B-Q6_K.gguf", TaskType: TaskEmbedding, Port: port, CtxSize: 4096, Threads: 8}
}

func TestProbeReportsDimensionWhenBothSidesReachable(t *testing.T) {
	_, port := fakeServer(t, 2560, http.StatusOK, http.StatusOK)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	result := prober.Check(context.Background(), embeddingSpec(port))
	if result.Reachability != ReachLoopback {
		t.Fatalf("reachability = %q, note = %q", result.Reachability, result.Note)
	}
	if result.EmbeddingDim != 2560 {
		t.Fatalf("dimension = %d", result.EmbeddingDim)
	}
}

func TestProbeRerankReportsReachability(t *testing.T) {
	_, port := fakeServer(t, 0, http.StatusOK, http.StatusOK)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	spec := Spec{ID: "rerank", Model: "qwen3-reranker-0.6b-q8_0.gguf", TaskType: TaskRerank, Port: port, CtxSize: 4096, Threads: 8}
	result := prober.Check(context.Background(), spec)
	if result.Reachability != ReachLoopback {
		t.Fatalf("reachability = %q, note = %q", result.Reachability, result.Note)
	}
}

// A chat instance is verified through the completion route and must not invent
// an embedding dimension it never measured.
func TestProbeChatUsesCompletionRoute(t *testing.T) {
	_, port := fakeServer(t, 0, http.StatusOK, http.StatusOK)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	spec := Spec{ID: "chat", Model: "Qwen3.5-9B-Q4_K_M.gguf", TaskType: TaskChat, Port: port, CtxSize: 4096, Threads: 8}
	result := prober.Check(context.Background(), spec)
	if result.Reachability != ReachLoopback {
		t.Fatalf("reachability = %q, note = %q", result.Reachability, result.Note)
	}
	if result.EmbeddingDim != 0 {
		t.Fatalf("chat probe reported an embedding dimension: %d", result.EmbeddingDim)
	}
}

// A chat instance pointed at a server that only serves embeddings must fail the
// task stage rather than pass on /health alone.
func TestProbeChatRejectsEmbeddingOnlyServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	spec := Spec{ID: "chat", Model: "c.gguf", TaskType: TaskChat, Port: portOf(t, server.URL), CtxSize: 4096, Threads: 8}
	result := prober.Check(context.Background(), spec)
	if result.Reachability == ReachLoopback {
		t.Fatalf("chat probe accepted a server with no completion route: %+v", result)
	}
}

func TestProbeStaysUnverifiedWithoutCrossOSConfirmation(t *testing.T) {
	_, port := fakeServer(t, 2560, http.StatusOK, http.StatusOK)
	cases := []struct {
		name  string
		probe func(context.Context, string) (int, error)
		want  string
	}{
		{"probe unavailable", func(context.Context, string) (int, error) { return 0, errors.New("wsl.exe not found") }, probeNoteWSLUnavailable},
		{"blocked from wsl", func(context.Context, string) (int, error) { return http.StatusBadGateway, nil }, probeNoteWSLBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prober := &Prober{WSLProbe: tc.probe}
			result := prober.Check(context.Background(), embeddingSpec(port))
			if result.Reachability != ReachUnverified {
				t.Fatalf("reachability = %q", result.Reachability)
			}
			if !strings.Contains(result.Note, tc.want) {
				t.Fatalf("note = %q, want it to mention %q", result.Note, tc.want)
			}
			if result.EmbeddingDim != 2560 {
				t.Fatalf("dimension = %d, want the task probe to still have run", result.EmbeddingDim)
			}
		})
	}
}

func TestProbeDetectsMissingTaskFlag(t *testing.T) {
	// A server started without --embedding answers /health but 404s the task
	// route. This is the failure the naive health check would miss.
	_, port := fakeServer(t, 2560, http.StatusOK, http.StatusNotFound)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	result := prober.Check(context.Background(), embeddingSpec(port))
	if result.Reachability != ReachUnverified {
		t.Fatalf("reachability = %q", result.Reachability)
	}
	if !strings.Contains(result.Note, probeNoteTaskFailed) {
		t.Fatalf("note = %q", result.Note)
	}
	if result.EmbeddingDim != 0 {
		t.Fatalf("dimension = %d, want 0 when the task route failed", result.EmbeddingDim)
	}
}

func TestProbeReportsHealthFailure(t *testing.T) {
	_, port := fakeServer(t, 2560, http.StatusServiceUnavailable, http.StatusOK)
	prober := &Prober{WSLProbe: func(context.Context, string) (int, error) { return http.StatusOK, nil }}
	result := prober.Check(context.Background(), embeddingSpec(port))
	if result.Reachability != ReachUnverified {
		t.Fatalf("reachability = %q", result.Reachability)
	}
	if !strings.Contains(result.Note, probeNoteHealthFailed) {
		t.Fatalf("note = %q", result.Note)
	}
}

func TestProbeReportsNothingListening(t *testing.T) {
	server, port := fakeServer(t, 2560, http.StatusOK, http.StatusOK)
	server.Close()
	prober := &Prober{DialTimeout: 200 * time.Millisecond}
	result := prober.Check(context.Background(), embeddingSpec(port))
	if result.Reachability != ReachUnverified {
		t.Fatalf("reachability = %q", result.Reachability)
	}
	if !strings.Contains(result.Note, probeNoteNotListening) {
		t.Fatalf("note = %q", result.Note)
	}
}

func TestObservePortsReportsOnlyListening(t *testing.T) {
	_, port := fakeServer(t, 0, http.StatusOK, http.StatusOK)
	closed, closedPort := fakeServer(t, 0, http.StatusOK, http.StatusOK)
	closed.Close()
	observed := ObservePorts(context.Background(), []int{closedPort, port}, nil)
	if len(observed) != 1 || observed[0] != port {
		t.Fatalf("observed = %v, want [%d]", observed, port)
	}
}

// The per-task budget has to reach the request. taskTimeout used to be consulted
// only for chat, so raising the embedding budget changed nothing and a warm
// server kept being reported unreachable.
func TestTaskClientAppliesPerTaskTimeout(t *testing.T) {
	prober := &Prober{}
	for _, task := range []TaskType{TaskEmbedding, TaskRerank, TaskChat} {
		if got, want := prober.taskClient(task).Timeout, task.taskTimeout(); got != want {
			t.Fatalf("%s client timeout = %s, want %s", task, got, want)
		}
	}
}
