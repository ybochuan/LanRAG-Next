package chatpipeline

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSearchParallelBridgeLiveDatabasePolicy(t *testing.T) {
	if os.Getenv("AGENTRAG_M2_LIVE_HOST") != "owned-harness" {
		t.Skip("real owned Host/SQLite cross-process harness only")
	}
	plugin := &PluginSearchParallel{searchPlugin: &PluginSearch{knowledgeBaseService: &policySearchService{&partialSearchKnowledgeBaseService{}}}, searchEntityPlugin: &PluginSearchEntity{}}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7, SessionID: "m2-live", KnowledgeBaseIDs: []string{"kb-good"}, KnowledgeIDs: []string{"active", "stale"}, SearchTargets: types.SearchTargets{{KnowledgeBaseID: "kb-good", Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"active", "stale"}}}, EmbeddingTopK: 10}, PipelineState: types.PipelineState{Intent: types.IntentKBSearch, RewriteQuery: "M2 LIVE DATABASE POLICY"}}
	calls := 0
	err := plugin.OnEvent(context.WithValue(context.Background(), types.RequestIDContextKey, "m2-live"), types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError { calls++; return nil })
	if err != nil || calls != 1 || len(cm.SearchResult) != 1 || cm.SearchResult[0].KnowledgeID != "active" {
		t.Fatalf("live database policy failed: result=%v next=%d hits=%d", err, calls, len(cm.SearchResult))
	}
	t.Log("PASS: completed Observer -> SQLite active/stale -> real Host decision -> native parallel search 4 hits -> only 1 active hit forwarded")
}
