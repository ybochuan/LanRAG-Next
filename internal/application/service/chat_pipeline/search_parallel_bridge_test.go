package chatpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type enumeratedKnowledgeService struct {
	interfaces.KnowledgeService
	docs []*types.Knowledge
}

func (s enumeratedKnowledgeService) ListKnowledgeByKnowledgeBaseID(context.Context, string) ([]*types.Knowledge, error) {
	return s.docs, nil
}

func TestWholeLibraryGovernanceRange(t *testing.T) {
	for _, count := range []int{200, 201, extensionbridge.MaxRetrieveDocuments + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			docs := make([]*types.Knowledge, count)
			for i := range docs {
				docs[i] = &types.Knowledge{ID: fmt.Sprintf("doc-%d", i), EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted}
			}
			docs[count-1].ID = "active"
			cm := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{SessionID: "s", TenantID: 7, KnowledgeBaseIDs: []string{"kb-good"}, SearchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, TenantID: 7, KnowledgeBaseID: "kb-good"}}, EmbeddingTopK: 10},
				PipelineState:   types.PipelineState{Intent: types.IntentKBSearch, RewriteQuery: "policy"},
			}
			plugin := &PluginSearchParallel{knowledgeService: enumeratedKnowledgeService{docs: docs}}
			ctx := context.WithValue(t.Context(), types.RequestIDContextKey, "range")
			if count > extensionbridge.MaxRetrieveDocuments {
				if err := plugin.OnEvent(ctx, types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { t.Fatal("oversized request continued"); return nil }); err == nil || !errors.Is(err.Err, extensionbridge.ErrRetrieveScopeTooLarge) {
					t.Fatalf("oversized request returned %+v", err)
				}
				return
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:8090")
			if err != nil {
				t.Skipf("8090 already in use: %v", err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					IDs []string `json:"knowledge_ids"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || len(payload.IDs) != count || payload.IDs[count-1] != "active" {
					t.Errorf("incomplete native scope: count=%d err=%v", len(payload.IDs), err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(extensionbridge.Decision{RequestID: "range", TenantID: 7, KnowledgeBaseIDs: []string{"kb-good"}, KnowledgeIDs: []string{"active"}, ExcludedDocumentIDs: payload.IDs[:count-1]})
			}))
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			t.Cleanup(server.Close)
			plugin.searchPlugin = &PluginSearch{knowledgeBaseService: &policySearchService{&partialSearchKnowledgeBaseService{}}}
			plugin.searchEntityPlugin = &PluginSearchEntity{}
			if err := plugin.OnEvent(ctx, types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { return nil }); err != nil || len(cm.SearchResult) != 1 || cm.SearchResult[0].KnowledgeID != "active" {
				t.Fatalf("governed search got %v, results=%v", err, cm.SearchResult)
			}
		})
	}
}

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
				if tc.status == 200 {
					json.NewEncoder(w).Encode(map[string]any{"request_id": "r", "tenant_id": 7, "knowledge_base_ids": []string{"kb"}, "knowledge_ids": []string{"doc"}, "excluded_document_ids": []string{}, "deny_all": false})
				}
			}))
			s.Listener.Close()
			s.Listener = listener
			s.Start()
			t.Cleanup(s.Close)
			if tc.stopped {
				s.Close()
			}
			cm := &types.ChatManage{
				PipelineRequest: types.PipelineRequest{SessionID: "session-bridge", TenantID: 7, KnowledgeBaseIDs: []string{"kb"}, KnowledgeIDs: []string{"doc"}},
				PipelineState:   types.PipelineState{Intent: types.IntentKBSearch, Entity: []string{"entity"}, SearchResult: []*types.SearchResult{{ID: "untouched"}}},
			}
			plugin := &PluginSearchParallel{searchPlugin: &PluginSearch{}, searchEntityPlugin: &PluginSearchEntity{}}
			nextCalls := 0
			start := time.Now()
			result := plugin.OnEvent(context.WithValue(context.Background(), types.RequestIDContextKey, "r"), types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { nextCalls++; return nil })
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
