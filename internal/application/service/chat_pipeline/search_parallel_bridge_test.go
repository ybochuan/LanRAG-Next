package chatpipeline

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSearchParallelBridgeGate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		stopped, slow bool
	}{
		{"unavailable", 0, true, false}, {"503", 503, false, false}, {"timeout", 200, false, true}, {"200", 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:8090")
			if err != nil {
				t.Skipf("8090 already owned; not contacting or stopping its owner: %v", err)
			}
			var requests atomic.Int32
			s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/bridge/before-retrieve" || r.Method != http.MethodPost || r.Header.Get("X-AgentRAG-Bridge-Version") != "1" {
					t.Error("wrong bridge request")
				}
				if tc.slow {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(2 * time.Second):
					}
				}
				w.WriteHeader(tc.status)
			}))
			s.Listener.Close()
			s.Listener = listener
			s.Start()
			t.Cleanup(s.Close)
			if tc.stopped {
				s.Close()
			}
			cm := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{SessionID: "session-bridge", TenantID: 7},
				PipelineState:   types.PipelineState{Intent: types.IntentKBSearch, Entity: []string{"entity"}, SearchResult: []*types.SearchResult{{ID: "untouched"}}},
			}
			plugin := &PluginSearchParallel{searchPlugin: &PluginSearch{}, searchEntityPlugin: &PluginSearchEntity{}}
			nextCalls := 0
			start := time.Now()
			result := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { nextCalls++; return nil })
			if tc.status == 200 && !tc.slow {
				// Original empty-search behavior proves HTTP 200 reached the native
				// pipeline; the gate itself does not invent search results.
				if result != ErrSearchNothing || len(cm.SearchResult) != 0 || requests.Load() != 1 {
					t.Fatalf("200 did not continue original search: %+v requests=%d", result, requests.Load())
				}
			} else {
				if result == nil || result == ErrSearchNothing || result.Err == nil || result.ErrorType != "bridge_before_retrieve_failed" || nextCalls != 0 || len(cm.SearchResult) != 1 || cm.SearchResult[0].ID != "untouched" {
					t.Fatalf("failure entered retrieval/fallback: %+v next=%d results=%v", result, nextCalls, cm.SearchResult)
				}
				if tc.slow && time.Since(start) > 1500*time.Millisecond {
					t.Fatal("bridge timeout exceeded test budget")
				}
			}
		})
	}
}

func TestSearchParallelBridgeKeepsNoRetrievalSkip(t *testing.T) {
	plugin := &PluginSearchParallel{}
	cm := &types.ChatManage{PipelineState: types.PipelineState{Intent: types.IntentChitchat}}
	calls := 0
	if err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("native no-retrieval skip changed: %+v calls=%d", err, calls)
	}
}
