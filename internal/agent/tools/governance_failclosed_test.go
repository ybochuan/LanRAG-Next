package tools

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"strings"
	"testing"
)

func TestGovernanceReadDocumentRequiresHostPermission(t *testing.T) {
	tool, repo := newReadDocumentFixture(2)
	result, err := tool.Execute(context.WithValue(context.Background(), governanceFixtureKey{}, true), json.RawMessage(`{"id":"doc-1"}`))
	if err == nil || (result != nil && result.Success) || len(repo.requests) != 0 {
		t.Fatal("read_document consumed document chunks without an active-state Host permission")
	}
}

func TestGovernanceWorkStatusKeepsControlWithoutUnprovedTextOrACK(t *testing.T) {
	h := &fakeWorkHandler{detailed: map[string]string{"sj_done": interfaces.WorkStateSucceeded}, cheap: map[string][]string{"sj_done": {interfaces.WorkStateSucceeded}}}
	marks := &recordingMarks{}
	tool, _ := newTestWorkWait(h, marks, nil)
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	r, e := tool.Execute(ctx, json.RawMessage(`{"work_ids":["sj_done"],"wait_sec":0}`))
	if e != nil || r == nil || !r.Success || !strings.Contains(r.Output, "succeeded") || strings.Contains(r.Output, "job ") || strings.Contains(r.Output, "out-") || len(marks.seen) != 0 {
		t.Fatal("unproved work text exposed or notice acknowledged")
	}
	for _, call := range h.calls {
		if call.Detailed || call.RunID != "" {
			t.Fatal("unproved output requested or cursor advanced")
		}
	}
}

func TestGovernanceOldChunkHandleRequiresFreshPermit(t *testing.T) {
	tool, repo := newReadDocumentFixture(2)
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"chunk-1","context":2}`))
	if err == nil || result.Success || len(repo.requests) != 0 {
		t.Fatal("old chunk handle bypassed current permission")
	}
}

func TestGovernanceFiltersBeforeRerankAndListing(t *testing.T) {
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 7}}
	rows := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "a", KnowledgeID: "active", KnowledgeBaseID: "kb", Content: "ACTIVE_BODY"}, KnowledgeBaseID: "kb"},
		{SearchResult: &types.SearchResult{ID: "b", KnowledgeID: "governance-revoked", KnowledgeBaseID: "kb", Content: "REVOKED_BODY"}, KnowledgeBaseID: "kb"},
	}
	kept, err := filterGovernedSearchResults(context.Background(), targets, nil, rows)
	if err != nil || len(kept) != 1 || kept[0].KnowledgeID != "active" {
		t.Fatal("revoked result reached the ranking input")
	}
	docs, err := filterGovernedDocuments(context.Background(), 7, "kb", []*types.Knowledge{
		{ID: "active", TenantID: 7, KnowledgeBaseID: "kb", Description: "ACTIVE_DESCRIPTION"},
		{ID: "governance-revoked", TenantID: 7, KnowledgeBaseID: "kb", Description: "REVOKED_DESCRIPTION"},
	})
	encoded, _ := json.Marshal(docs)
	if err != nil || len(docs) != 1 || strings.Contains(string(encoded), "REVOKED_DESCRIPTION") {
		t.Fatal("revoked metadata reached tool listing")
	}
}

func TestGovernanceUnattributedReadsRejectBeforeDependencies(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	calls := []func(context.Context, json.RawMessage) (*types.ToolResult, error){
		(&ReadPageTool{}).Execute, (&FillPageTool{}).Execute, (&SelectTabTool{}).Execute,
		(&DatabaseQueryTool{}).Execute, (&SearchConversationsTool{}).Execute,
		(&QueryKnowledgeGraphTool{}).Execute, (&wikiReadPageTool{}).Execute,
		(&wikiSearchTool{}).Execute, (&RunOperationTool{}).Execute,
		(&wikiDeletePageTool{}).Execute,
		(&wikiFlagIssueTool{}).Execute,
		(&wikiRenamePageTool{}).Execute,
		(&wikiReplaceTextTool{}).Execute,
		(&wikiUpdateIssueTool{}).Execute,
		(&wikiWritePageTool{}).Execute,
		(&ShellExecTool{}).Execute,
		(&ShellExecAsyncTool{}).Execute,
		(&SearchMemoryTool{}).Execute,
		(&ListSandboxFilesTool{}).Execute,
		(&WebFetchTool{}).Execute,
		(&EditSandboxFileTool{}).Execute,
	}
	for _, call := range calls {
		result, err := call(ctx, json.RawMessage(`{}`))
		if err == nil || result == nil || result.Success {
			t.Fatal("unattributed read was not refused")
		}
	}
}

func TestGovernanceUnknownFileCacheArtifactAndSkillAreBlocked(t *testing.T) {
	ctx := context.WithValue(context.Background(), governanceFixtureKey{}, true)
	reader := NewReadFileTool(nil)
	for _, path := range []string{"resource://old/file", "skill://old/SKILL.md", "web://old"} {
		switch {
		case strings.HasPrefix(path, "resource:"):
			result := reader.readArtifact(ctx, ReadFileInput{Path: path})
			if result == nil || result.Success || !strings.Contains(result.Error, "provenance") {
				t.Fatal("unknown artifact not refused by policy")
			}
		case strings.HasPrefix(path, "skill:"):
			result := reader.readSkillResource(ctx, ReadFileInput{Path: path})
			if result == nil || result.Success || !strings.Contains(result.Error, "provenance") {
				t.Fatal("unknown skill not refused by policy")
			}
		default:
			result := reader.readWebPage(ctx, ReadFileInput{Path: path})
			if result == nil || result.Success || !strings.Contains(result.Error, "provenance") {
				t.Fatal("unknown cached page not refused by policy")
			}
		}
	}
	cache := &workspaceFileReader{}
	if data, e := cache.readWithCache(ctx, "session", "/workspace/old", nil); e == nil || data != nil {
		t.Fatal("unknown cache read passed")
	}
}

func TestGovernanceShellCannotExecuteBeforeSourceProof(t *testing.T) {
	executor := &fakeShellExecutor{}
	ctx := context.WithValue(shellExecTestContext(), governanceFixtureKey{}, true)
	r, e := NewShellExecTool(executor, nil).Execute(ctx, json.RawMessage(`{"command":"cat /workspace/private.txt"}`))
	if e == nil || r == nil || r.Success || executor.calls != 0 {
		t.Fatal("unknown shell output can execute or reach a model")
	}
}
