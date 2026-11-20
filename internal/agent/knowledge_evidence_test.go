package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type evidenceSearchStub struct {
	agenttools.BaseTool
	queries []string
	count   int
}

func (t *evidenceSearchStub) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input agenttools.SearchKnowledgeInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	t.queries = append(t.queries, input.Query)
	return &types.ToolResult{Success: true, Output: "search result", Data: map[string]interface{}{"count": t.count}}, nil
}

func TestKnowledgeEvidenceRoutesBeforeFirstAnswer(t *testing.T) {
	const question = "The reimbursement policy in the selected document: how many reviewers are required?"
	for _, tc := range []struct {
		name, query, prior, search string
		selected                   bool
		count                      int
		archive                    bool
	}{
		{name: "empty native result", query: question, selected: true, search: question},
		{name: "nonempty without acknowledged receipt", query: question, selected: true, search: question, count: 1},
		{name: "verified follow-up without explicit selection", query: "那从哪天算？", prior: "报销制度里的五个工作日适用哪一条？", search: "报销制度里的五个工作日适用哪一条？\n那从哪天算？"},
		{name: "greeting with KB selected", query: "您好！", selected: true},
		{name: "existing authorized archive route", query: "仅查阅已经失效的历史条款，提醒我它是否仍有效。", selected: true, archive: true},
		{name: "archive route unavailable", query: "仅查阅已经失效的历史条款，提醒我它是否仍有效。", selected: true, search: "仅查阅已经失效的历史条款，提醒我它是否仍有效。"},
		{name: "general chat without selection", query: "What is a reimbursement policy?"},
		{name: "follow-up without verified cue", query: "那从哪天算？"},
		{name: "follow-up with explicit scope but no verified cue", query: "那从哪天算？", selected: true, search: "那从哪天算？"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{{
				ResponseType: types.ResponseTypeAnswer, Content: "ordinary response", Done: true, FinishReason: "stop",
			}}}}}
			e := newTestEngine(t, model)
			e.toolRegistry = agenttools.NewToolRegistry()
			search := &evidenceSearchStub{
				BaseTool: agenttools.NewBaseTool(agenttools.ToolSearchKnowledge, "native search stub", json.RawMessage(`{"type":"object"}`)),
				count:    tc.count,
			}
			e.toolRegistry.RegisterTool(search)
			completions := 0
			e.eventBus.On(event.EventAgentComplete, func(_ context.Context, _ event.Event) error { completions++; return nil })
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
			if tc.archive {
				key := strings.Repeat("x", 32)
				t.Setenv("AGENTRAG_MANAGEMENT_KEY", key)
				url := "http://127.0.0.1:8090/mcp"
				service := &types.MCPService{ID: "archive", Enabled: true, TenantID: 7, URL: &url,
					TransportType: types.MCPTransportHTTPStreamable, Headers: types.MCPHeaders{"Authorization": "Bearer " + key}}
				scoped := agenttools.RelatedMCPServiceScope(service, types.SearchTargets{
					{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb", TenantID: 7, KnowledgeIDs: []string{"doc"}},
				})
				_, err := agenttools.RegisterMCPTools(ctx, e.toolRegistry, []*types.MCPService{scoped}, nil, nil, 0, nil, nil)
				require.NoError(t, err)
			}
			ctx = WithKnowledgeEvidence(ctx, tc.selected, tc.query, tc.prior)
			state := &types.AgentState{}
			messages := []chat.Message{{Role: "user", Content: tc.query}}
			_, err := e.executeLoop(ctx, state, tc.query, messages, e.buildToolsForLLM(), "session", "message")
			require.NoError(t, err)
			require.Equal(t, 1, completions)
			if tc.search == "" {
				require.Empty(t, search.queries)
				require.Equal(t, "ordinary response", state.FinalAnswer)
				require.Equal(t, 1, model.callCount)
				require.Empty(t, model.opts[0].ToolChoice)
				return
			}
			require.Equal(t, []string{tc.search}, search.queries)
			require.True(t, state.IsComplete)
			require.NotEmpty(t, state.FinalAnswer)
			require.Zero(t, model.callCount)
		})
	}
}

func TestCurrentSearchReceiptJoinsCorrectionToOnlyMatchingCurrentChunk(t *testing.T) {
	e := newTestEngine(t, &mockChat{})
	const (
		callID  = "call-test"
		chunkID = "1a3fac35-ef12-4dc8-b24a-07a5be4ea152"
		docID   = "11245c1a-a00d-48cf-ae64-77794cca9f66"
		kbID    = "314460a1-8dc9-438c-b999-b2afb730a86c"
	)
	result := &types.ToolResult{Success: true, Data: map[string]interface{}{
		"display_type": "search_results",
		"count":        1,
		"results": []map[string]interface{}{
			{"chunk_id": chunkID, "knowledge_id": docID, "knowledge_base_id": kbID, "content": "current original"},
		},
	}}
	body, err := json.Marshal(result)
	require.NoError(t, err)
	output := extensionbridge.ConsumedToolOutput{StepID: "step", ToolCallID: callID, OutputHash: extensionbridge.SourceHash(append(body, '\n'))}
	e.searchOutputs.byCall = map[string]searchOutputReceipt{callID: {output: output, runID: "run", result: result}}
	step := types.AgentStep{ToolCalls: []types.ToolCall{{ID: callID, Name: agenttools.ToolSearchKnowledge, Result: result}}}
	messages := e.modelContext.EncodeMessages(e.appendToolResults([]chat.Message{{Role: "user", Content: "policy?"}}, step))
	require.Len(t, messages, 3)
	require.Equal(t, "assistant", messages[1].Role)
	require.Empty(t, messages[1].Content)
	require.Equal(t, callID, messages[1].ToolCalls[0].ID)
	require.Equal(t, "tool", messages[2].Role)
	require.Equal(t, callID, messages[2].ToolCallID)
	require.Contains(t, messages[2].Content, "current original")
	require.Equal(t, extensionbridge.SourceFieldHash(messages[2].Content), e.searchOutputs.byCall[callID].renderedHash)
	correction := types.CorrectionReference{ID: "reviewed", DocumentID: docID, ChunkID: chunkID, VersionID: "v1", Revision: 2}
	wrongDoc := types.CorrectionReference{ID: "wrong", DocumentID: "another-doc", ChunkID: chunkID, Revision: 1}
	refs := e.searchOutputs.references([]extensionbridge.ConsumedToolOutput{output}, []types.CorrectionReference{correction, wrongDoc})
	require.Len(t, refs, 1)
	require.Equal(t, []types.CorrectionReference{correction}, refs[0].CorrectionItems)
	output.OutputHash = "modified"
	require.Empty(t, e.searchOutputs.references([]extensionbridge.ConsumedToolOutput{output}, []types.CorrectionReference{correction}))
}
