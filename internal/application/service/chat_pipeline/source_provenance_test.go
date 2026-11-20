package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceInputsBindActualBodyTitleAndDescription(t *testing.T) {
	cm := &types.ChatManage{}
	cm.MergeResult = []*types.SearchResult{{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "BODY", KnowledgeTitle: "TITLE", KnowledgeDescription: "DESCRIPTION"}}
	inputs, complete := sourceInputsForModel(cm)
	if !complete || len(inputs) != 3 {
		t.Fatalf("raw fields were not covered: %+v", inputs)
	}
	want := map[string]string{"content": extensionbridge.SourceFieldHash("BODY"), "title": extensionbridge.SourceFieldHash("TITLE"), "description": extensionbridge.SourceFieldHash("DESCRIPTION")}
	for _, input := range inputs {
		if input.DocumentID != "doc" || input.ValueHash != want[input.Field] || (input.Field == "content") != (input.ChunkID == "chunk") {
			t.Fatalf("wrong native field binding: %+v", input)
		}
	}
	// Selecting and reordering candidates does not change the bytes that reach
	// the model, so a configured rerank model keeps the same native bindings.
	withRerank := *cm
	withRerank.RerankModelID = "rerank-model"
	withRerank.RerankResult = []*types.SearchResult{{ID: "chunk", KnowledgeID: "doc", Content: "BODY"}}
	reranked, complete := sourceInputsForModel(&withRerank)
	if !complete || len(reranked) != 3 {
		t.Fatalf("rerank hid native bindings: complete=%v inputs=%+v", complete, reranked)
	}
	for _, input := range reranked {
		if input.DocumentID != "doc" || input.ValueHash != want[input.Field] {
			t.Fatalf("rerank changed a native binding: %+v", input)
		}
	}
	for _, mode := range []string{"rewritten", "image", "metadata", "web", "faq", "history", "query-rewrite"} {
		t.Run(mode, func(t *testing.T) {
			copyCM := *cm
			result := *cm.MergeResult[0]
			copyCM.MergeResult = []*types.SearchResult{&result}
			switch mode {
			case "rewritten":
				result.ContentRewritten = true
			case "image":
				result.ImageInfo = `[{"caption":"UNKNOWN_IMAGE"}]`
			case "metadata":
				result.KnowledgeCustomMetadata = "FORMATTED_METADATA"
			case "web":
				result.MatchType = types.MatchTypeWebSearch
			case "faq":
				result.ChunkType = string(types.ChunkTypeFAQ)
			case "history":
				copyCM.History = []*types.History{{}}
			case "query-rewrite":
				// The rewritten query is model output, not a native field, so a
				// model sees bytes no native snapshot can vouch for.
				copyCM.EnableRewrite = true
			}
			if _, known := sourceInputsForModel(&copyCM); known {
				t.Fatal("unproved composition was declared complete")
			}
		})
	}
}

type controlledSourceChat struct {
	openStreamChat
	stream chan types.StreamResponse
	called bool
	rewriteMessages []chat.Message
	answerMessages []chat.Message
}

func (m *controlledSourceChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.rewriteMessages = messages
	return &types.ChatResponse{Content: `{"rewrite_query":"retrieval variant","intent":"kb_search"}`}, nil
}

func (m *controlledSourceChat) ChatStream(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	m.called = true
	m.answerMessages = messages
	return m.stream, nil
}

// This is a transport fixture only. The real Host/SQLite verifier is covered by
// extensions/lineage; no test authority is installed in a production constructor.
func TestStreamingSourceProofWaitsForTerminalAndBlocksUnverifiedInput(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "proof-failure", "default-rewrite-neighbors"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("k", 32))
			listener, err := net.Listen("tcp", "127.0.0.1:8090")
			if err != nil {
				t.Fatal(err)
			}
			identity := extensionbridge.SourceRunIdentity{Scope: extensionbridge.SourceScope{TenantID: 7, KnowledgeBaseID: "kb"}, UserID: "user", SessionID: "session", AttemptID: "native-message", AuthorizedDocumentIDs: []string{"doc"}}
			finished := make(chan bool, 1)
			var mu sync.Mutex
			var outcome string
			var stepCount int
			var modelInputs []extensionbridge.SourceInput
			var modelOutcomes []string
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					RequestID string                      `json:"request_id"`
					Scope     extensionbridge.SourceScope `json:"scope"`
					Action    string                      `json:"action"`
					Input *struct {
						Evidence extensionbridge.SourceInput `json:"evidence"`
					} `json:"input"`
					Result    *struct {
						Outcome string `json:"outcome"`
					} `json:"result"`
					Finish *struct {
						Abort            bool  `json:"abort"`
						ExpectedRevision int64 `json:"expected_revision"`
					} `json:"finish"`
				}
				if json.NewDecoder(r.Body).Decode(&q) != nil || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("k", 32) {
					w.WriteHeader(401)
					return
				}
				if mode == "proof-failure" && r.URL.Path == "/provenance/runs/inputs" {
					w.WriteHeader(503)
					return
				}
				var data any = struct{}{}
				switch r.URL.Path {
				case "/provenance/runs/begin":
					data = extensionbridge.SourceRunHandle{Scope: q.Scope, RunID: "run", Capability: strings.Repeat("b", 64)}
				case "/provenance/runs/steps":
					if q.Action == "begin" {
						mu.Lock()
						stepCount++
						data = map[string]string{"step_id": fmt.Sprintf("step-%d", stepCount)}
						mu.Unlock()
					} else {
						mu.Lock()
						outcome = q.Result.Outcome
						modelOutcomes = append(modelOutcomes, outcome)
						mu.Unlock()
					}
				case "/provenance/runs/inputs":
					mu.Lock()
					modelInputs = append(modelInputs, q.Input.Evidence)
					mu.Unlock()
				case "/provenance/runs/finish":
					state, revision := "open", int64(7)
					if q.Finish != nil {
						state = "sealed"
						if q.Finish.Abort {
							state = "aborted"
						}
						revision++
					}
					data = map[string]any{"identity": identity, "run_id": "run", "revision": revision, "state": state, "sources": []extensionbridge.SourceRef{{DocumentID: "doc", VersionID: "version"}}, "created_at": time.Now().UTC(), "expires_at": time.Now().UTC().Add(time.Hour)}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "request_id": q.RequestID, "scope": q.Scope, "data": data})
				if q.Finish != nil {
					finished <- q.Finish.Abort
				}
			})}
			go server.Serve(listener)
			t.Cleanup(func() { _ = server.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, err = extensionbridge.BeginSourceRun(ctx, identity)
			if err != nil {
				t.Fatal(err)
			}
			model := &controlledSourceChat{stream: make(chan types.StreamResponse, 1)}
			cm := &types.ChatManage{}
			cm.SessionID = "session"
			cm.UserContent = "ACTUAL_PROMPT"
			cm.EventBus = &syncEventBus{}
			cm.MergeResult = []*types.SearchResult{{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "BODY"}}
			if mode == "default-rewrite-neighbors" {
				cm.Query = "original question"
				cm.EnableRewrite = true
				cm.EnableQueryExpansion = true
				cm.RewritePromptUser = "{{query}}"
				cm.SummaryConfig.ContextTemplate = "{{query}}\n{{contexts}}"
				cm.SearchResult = []*types.SearchResult{{ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "BODY", ChunkType: string(types.ChunkTypeText)}}
				rewriter := &PluginQueryUnderstand{modelService: &stubModelService{model: model}, config: &config.Config{Conversation: &config.ConversationConfig{}}}
				repo := &expandChunkRepo{chunks: map[string]*types.Chunk{
					"chunk": {ID: "chunk", KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeText, Content: "BODY", IsEnabled: true, NextChunkID: "neighbor", ChunkIndex: 1},
					"neighbor": {ID: "neighbor", KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeText, Content: "NEIGHBOR BODY", IsEnabled: true, ChunkIndex: 2, ContentRevision: 3},
				}}
				require.Nil(t, rewriter.OnEvent(ctx, types.QUERY_UNDERSTAND, cm, func() *PluginError { return nil }))
				require.Equal(t, "retrieval variant", cm.RewriteQuery)
				require.Contains(t, model.rewriteMessages[1].Content, "original question")
				require.NotContains(t, model.rewriteMessages[1].Content, "NEIGHBOR BODY")
				merger := &PluginMerge{chunkRepo: repo}
				require.Nil(t, merger.OnEvent(ctx, types.CHUNK_MERGE, cm, func() *PluginError { return nil }))
				prompt := &PluginIntoChatMessage{}
				require.Nil(t, prompt.OnEvent(ctx, types.INTO_CHAT_MESSAGE, cm, func() *PluginError { return nil }))
			}
			plugin := &PluginChatCompletionStream{modelService: &stubModelService{model: model}}
			pluginErr := plugin.OnEvent(ctx, types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil })
			if mode == "proof-failure" {
				if pluginErr == nil || model.called {
					t.Fatal("model started after proof transport failure")
				}
				return
			}
			if pluginErr != nil {
				t.Fatal(pluginErr)
			}
			select {
			case <-finished:
				t.Fatal("run finished before the stream ended")
			default:
			}
			if mode == "cancel" {
				cancel()
			} else {
				model.stream <- types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: "OUTPUT", Done: true, FinishReason: "stop"}
				close(model.stream)
			}
			select {
			case abort := <-finished:
				mu.Lock()
				got := outcome
				mu.Unlock()
				if mode == "default-rewrite-neighbors" {
					final := model.answerMessages[len(model.answerMessages)-1].Content
					require.Contains(t, final, "original question")
					require.Contains(t, final, "NEIGHBOR BODY")
					require.NotContains(t, final, "retrieval variant")
					require.Equal(t, []string{"success", "success"}, modelOutcomes)
					chunkHashes := map[string]string{}
					for _, source := range modelInputs {
						if source.Field == "content" {
							require.Equal(t, "doc", source.DocumentID)
							chunkHashes[source.ChunkID] = source.ValueHash
						}
					}
					require.Equal(t, map[string]string{"chunk": extensionbridge.SourceFieldHash("BODY"), "neighbor": extensionbridge.SourceFieldHash("NEIGHBOR BODY")}, chunkHashes)
					require.False(t, abort)
					return
				}
				if abort != (mode == "cancel") || (got == "success") != (mode == "success") {
					t.Fatalf("terminal outcome=%s abort=%v", got, abort)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("actual terminal did not finish provenance")
			}
		})
	}
}
