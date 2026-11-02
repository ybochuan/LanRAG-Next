package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"strings"
	"testing"
)

func TestGovernancePromptDropsRevokedBodyAndMetadata(t *testing.T) {
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7}, PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{
		{KnowledgeID: "active", KnowledgeBaseID: "kb", Content: "ACTIVE_BODY", KnowledgeDescription: "ACTIVE_DESCRIPTION"},
		{KnowledgeID: "governance-revoked", KnowledgeBaseID: "kb", Content: "REVOKED_BODY", KnowledgeDescription: "REVOKED_DESCRIPTION"},
	}}}
	if err := guardPromptResults(context.Background(), cm); err != nil {
		t.Fatal(err)
	}
	if len(cm.MergeResult) != 1 || strings.Contains(buildDocumentHeader(cm.MergeResult), "REVOKED") {
		t.Fatal("revoked metadata survived final prompt gate")
	}
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	if err := guardPromptResults(ctx, cm); err == nil {
		t.Fatal("prompt policy failed open")
	}
}

func TestGovernanceUsesAuthorizedMultiKBScopeAndPreservesNativeWeb(t *testing.T) {
	original := extensionbridge.EvaluateDocuments
	t.Cleanup(func() { extensionbridge.EvaluateDocuments = original })
	extensionbridge.EvaluateDocuments = func(_ context.Context, tenant uint64, kbs, ids []string) (map[string]bool, error) {
		if tenant != 7 || len(kbs) != 2 || kbs[0] != "kb-a" || kbs[1] != "kb-b" || len(ids) != 1 || ids[0] != "active" {
			t.Fatalf("incorrect native authority: %v %v", kbs, ids)
		}
		return map[string]bool{"active": true}, nil
	}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{TenantID: 7, KnowledgeBaseIDs: []string{"kb-a", "kb-b"}}, PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{
		{KnowledgeID: "active", Content: "AUTHORIZED_WITHOUT_RESULT_KB"},
		{MatchType: types.MatchTypeWebSearch, Content: "NATIVE_WEB"},
	}}}
	if err := guardPromptResults(context.Background(), cm); err != nil || len(cm.MergeResult) != 2 {
		t.Fatalf("scope/web regression: %v", err)
	}
}

func TestGovernanceRejectsOldAnswerAffinityBeforeQueryRewrite(t *testing.T) {
	plugin := &PluginQueryUnderstand{memoryService: &stubRetrievalMemory{retrieval: interfaces.RetrievalContext{Documents: []string{"REVOKED_TITLE_CANARY"}}}}
	if !strings.Contains(plugin.memoryBackground(context.Background(), &types.ChatManage{}), "REVOKED_TITLE_CANARY") {
		t.Fatal("positive memory fixture missing")
	}
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	if got := plugin.memoryBackground(ctx, &types.ChatManage{}); got != "" {
		t.Fatal("old-answer title reached rewrite background")
	}
}
