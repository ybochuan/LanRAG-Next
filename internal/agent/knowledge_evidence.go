package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

type knowledgeEvidenceKey struct{}
type currentKnowledgeEvidenceKey struct{}

var errNoCurrentKnowledgeEvidence = errors.New("no current knowledge search receipt for answer generation")

type knowledgeEvidenceRequest struct {
	selected bool
	query    string
	previous string
}

// WithKnowledgeEvidence carries the original user query, explicit scope selection,
// and a previous user query whose retrieval source is still authorized.
func WithKnowledgeEvidence(ctx context.Context, selected bool, query, previousQuery string) context.Context {
	return context.WithValue(ctx, knowledgeEvidenceKey{}, knowledgeEvidenceRequest{selected, query, previousQuery})
}

// IsKnowledgeFollowUp recognizes elliptical requests that need a prior topic.
// Callers must independently verify the source of the prior retrieval.
func IsKnowledgeFollowUp(query string) bool {
	q := evidenceQueryPrefix(query)
	for _, prefix := range []string{"那", "那么", "所以", "接着", "刚才", "上面", "这条", "该条", "这个", "这项",
		"then ", "and ", "so ", "what about ", "how about ", "what if ", "does that ", "is that ", "when does it ", "when do they "} {
		if strings.HasPrefix(q, prefix) {
			return true
		}
	}
	return false
}

func evidenceQueryPrefix(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, prefix := range []string{"请问", "请", "please "} {
		query = strings.TrimSpace(strings.TrimPrefix(query, prefix))
	}
	return query
}

func isKnowledgeGreeting(query string) bool {
	q := evidenceQueryPrefix(query)
	plain := strings.TrimFunc(q, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
	switch plain {
	case "", "你好", "您好", "在吗", "谢谢", "好的", "明白了", "早上好", "晚上好", "你好呀", "你好吗", "你是谁",
		"hi", "hello", "hey", "thanks", "thank you", "good morning", "good evening", "how are you", "who are you":
		return true
	}
	return false
}

func requestsArchiveSearch(query string) bool {
	q := evidenceQueryPrefix(query)
	for _, prefix := range []string{"仅查阅", "只查阅", "查阅", "查找", "查询", "查看", "find ", "look up ", "show me ", "search ", "retrieve "} {
		if !strings.HasPrefix(q, prefix) {
			continue
		}
		for _, archive := range []string{"历史归档", "历史条款", "失效条款", "已归档", "已废止", "archived ", "historical clause", "repealed ", "expired clause"} {
			if strings.Contains(q, archive) {
				return true
			}
		}
	}
	return false
}

// initialKnowledgeEvidence uses the same execution and transcript paths as a
// model-issued search. The assistant message is only the tool protocol envelope.
func (e *AgentEngine) initialKnowledgeEvidence(
	ctx context.Context, state *types.AgentState, messages []chat.Message, sessionID, messageID string,
) (context.Context, []chat.Message, bool) {
	req, _ := ctx.Value(knowledgeEvidenceKey{}).(knowledgeEvidenceRequest)
	query := strings.TrimSpace(req.query)
	previous := strings.TrimSpace(req.previous)
	if (!req.selected && previous == "") || isKnowledgeGreeting(query) {
		return ctx, messages, true
	}
	if e.toolRegistry == nil {
		return ctx, messages, false
	}
	if requestsArchiveSearch(query) && e.toolRegistry.HasScopedArchiveService(ctx) {
		return ctx, messages, true
	}
	if extensionbridge.CorrectionScopeUnsupported(ctx) {
		return ctx, messages, false
	}
	if IsKnowledgeFollowUp(query) && previous != "" {
		query = previous + "\n" + query
	}
	if ctx.Err() != nil {
		return ctx, messages, true
	}
	if _, err := e.toolRegistry.GetTool(agenttools.ToolSearchKnowledge); err != nil {
		return ctx, messages, false
	}
	args, _ := json.Marshal(agenttools.SearchKnowledgeInput{Query: query})
	response := &types.ChatResponse{ToolCalls: []types.LLMToolCall{{
		ID:       "call_" + uuid.NewString(),
		Type:     "function",
		Function: types.FunctionCall{Name: agenttools.ToolSearchKnowledge, Arguments: string(args)},
		// Gemini accepts this sentinel for externally authored function calls.
		// Other transports ignore the Google-specific metadata key.
		ProviderMetadata: types.ToolCallMetadata{"thought_signature": json.RawMessage(`"skip_thought_signature_validator"`)},
	}}}
	step := types.AgentStep{Iteration: state.CurrentRound, Timestamp: time.Now()}
	ctx = extensionbridge.WithCorrectionQuery(ctx, query, req.query)
	searchCtx := agenttools.WithCorrectionSourceSearch(ctx)
	if e.runRoundTools(searchCtx, state, response, &step, sessionID, messageID) {
		return ctx, messages, true
	}
	state.RoundSteps = append(state.RoundSteps, step)
	if ctx.Err() != nil {
		return ctx, messages, true
	}
	for _, call := range step.ToolCalls {
		if call.Result == nil || !call.Result.Success {
			continue
		}
		if count, _ := call.Result.Data["count"].(int); count > 0 {
			withSearch := e.appendToolResults(messages, step)
			if len(e.searchOutputs.consumed(ctx, e.modelContext.EncodeMessages(withSearch))) == 0 {
				return ctx, messages, false
			}
			ctx = context.WithValue(ctx, currentKnowledgeEvidenceKey{}, true)
			return ctx, e.appendToolImages(ctx, withSearch, step), true
		}
	}
	return ctx, messages, false
}

func (e *AgentEngine) publishSearchReferences(ctx context.Context, state *types.AgentState, sessionID string, refs []*types.SearchResult) {
	if len(refs) == 0 {
		return
	}
	state.KnowledgeRefs = refs
	_ = e.eventBus.Emit(ctx, event.Event{
		ID: generateEventID("references"), Type: event.EventAgentReferences, SessionID: sessionID,
		Data: event.AgentReferencesData{References: refs},
	})
}

func (e *AgentEngine) finishWithoutKnowledgeEvidence(ctx context.Context, state *types.AgentState, sessionID string) {
	answer := "The search did not provide usable evidence from the selected knowledge sources. I can’t verify an answer from them."
	chinese := strings.HasPrefix(types.LanguageFromContextOrDefault(ctx), "zh")
	if chinese {
		answer = "本轮未从选定的知识来源取得可用依据，无法据此确认答案。"
	}
	var check string
	if extensionbridge.CorrectionScopeUnsupported(ctx) {
		check = "unsupported_scope"
		answer = "Evidence-verified answers require a single knowledge base. Select one and ask again."
		if chinese {
			answer = "当前证据核验仅支持单一知识库，请选择一个知识库后重试。"
		}
	}
	state.FinalAnswer = answer
	state.IsComplete = true
	_ = e.eventBus.Emit(ctx, event.Event{
		ID: generateEventID("answer"), Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: answer, Done: true, CorrectionCheck: check},
	})
}
