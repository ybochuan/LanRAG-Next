package chatpipeline

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
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
	for _, mode := range []string{"rewritten", "image", "metadata", "web", "faq", "history"} {
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
}

func (m *controlledSourceChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	m.called = true
	return m.stream, nil
}

// This is a transport fixture only. The real Host/SQLite verifier is covered by
// extensions/lineage; no test authority is installed in a production constructor.
func TestStreamingSourceProofWaitsForTerminalAndBlocksUnverifiedInput(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "proof-failure"} {
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
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					RequestID string                      `json:"request_id"`
					Scope     extensionbridge.SourceScope `json:"scope"`
					Action    string                      `json:"action"`
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
						data = map[string]string{"step_id": "step"}
					} else {
						mu.Lock()
						outcome = q.Result.Outcome
						mu.Unlock()
					}
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
				if abort != (mode == "cancel") || (got == "success") != (mode == "success") {
					t.Fatalf("terminal outcome=%s abort=%v", got, abort)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("actual terminal did not finish provenance")
			}
		})
	}
}
