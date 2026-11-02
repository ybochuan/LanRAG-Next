package chatpipeline

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type policySearchService struct {
	*partialSearchKnowledgeBaseService
}

func (s *policySearchService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return []*types.SearchResult{{ID: "blocked-1", KnowledgeID: "stale", Content: "EXPIRED FACT MUST NOT LEAVE SEARCH"}, {ID: "allowed", KnowledgeID: "active", Content: "CURRENT POLICY FACT"}, {ID: "blocked-2", KnowledgeID: "stale", Content: "ANOTHER EXPIRED CHUNK"}, {ID: "unjudged", KnowledgeID: "outside-request", Content: "UNJUDGED DOCUMENT MUST NOT LEAVE SEARCH"}}, nil
}

func TestSearchParallelBridgePolicyActuallyFiltersNativeResults(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "deny_all"}[deny], func(t *testing.T) {
			l, err := net.Listen("tcp4", "127.0.0.1:8090")
			if err != nil {
				t.Skipf("8090 owned; not touching owner: %v", err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				allowed := []string{"active"}
				excluded := []string{"stale"}
				if deny {
					allowed = []string{}
					excluded = append(excluded, "active")
				}
				json.NewEncoder(w).Encode(map[string]any{"request_id": "policy-test", "tenant_id": 7, "knowledge_base_ids": []string{"kb-good"}, "knowledge_ids": allowed, "excluded_document_ids": excluded, "deny_all": deny})
			}))
			server.Listener.Close()
			server.Listener = l
			server.Start()
			defer server.Close()
			plugin := &PluginSearchParallel{searchPlugin: &PluginSearch{knowledgeBaseService: &policySearchService{&partialSearchKnowledgeBaseService{}}}, searchEntityPlugin: &PluginSearchEntity{}}
			if deny {
				plugin = &PluginSearchParallel{}
			} // Any invocation of search dependencies would panic.
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7, SessionID: "s", KnowledgeBaseIDs: []string{"kb-good"}, KnowledgeIDs: []string{"active", "stale"}, SearchTargets: types.SearchTargets{{KnowledgeBaseID: "kb-good", Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"active", "stale"}}}, EmbeddingTopK: 10}, PipelineState: types.PipelineState{Intent: types.IntentKBSearch, RewriteQuery: "policy"}}
			next := 0
			result := plugin.OnEvent(context.WithValue(context.Background(), types.RequestIDContextKey, "policy-test"), types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { next++; return nil })
			if deny {
				if result != ErrSearchNothing || next != 0 || len(cm.SearchResult) != 0 {
					t.Fatalf("deny result=%v next=%d hits=%v", result, next, cm.SearchResult)
				}
				return
			}
			if result != nil || next != 1 || len(cm.SearchResult) != 1 || cm.SearchResult[0].KnowledgeID != "active" {
				t.Fatalf("filter result=%v next=%d hits=%+v", result, next, cm.SearchResult)
			}
		})
	}
}

func TestBridgeFilterCoversBothChunkAndEntityHits(t *testing.T) {
	merged := []*types.SearchResult{{ID: "chunk-stale", KnowledgeID: "stale"}, {ID: "entity-stale", KnowledgeID: "stale"}, {ID: "active", KnowledgeID: "active"}, {ID: "unknown", KnowledgeID: "outside-request"}, {ID: "web", KnowledgeID: "https://example.test", MatchType: types.MatchTypeWebSearch}}
	got := filterBridgeResults(merged, []string{"active"}, []string{"stale"})
	if len(got) != 2 || got[0].KnowledgeID != "active" || got[1].ID != "web" {
		t.Fatal(got)
	}
}
