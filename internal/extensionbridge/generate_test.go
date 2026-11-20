package extensionbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

func TestGuidanceIsEphemeralBoundToActualRequestAndFailsOpen(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("k", 32))
	original := sourceHTTPClient
	t.Cleanup(func() { sourceHTTPClient = original })
	h := SourceRunHandle{Scope: SourceScope{TenantID: 7, KnowledgeBaseID: "kb"}, RunID: "run", Capability: strings.Repeat("b", 64)}
	ctx := context.WithValue(context.Background(), sourceRunKey{}, h)
	for _, mode := range []string{"valid", "unattributed", "503", "timeout", "scope", "oversize", "duplicate", "annotation", "mixed", "bad-annotation", "correction", "correction-after-tool"} {
		t.Run(mode, func(t *testing.T) {
			correction := strings.HasPrefix(mode, "correction")
			sourceHTTPClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: provenanceRoundTrip(func(req *http.Request) (*http.Response, error) {
				var q sourceRequest
				if err := json.NewDecoder(req.Body).Decode(&q); err != nil {
					t.Fatal(err)
				}
				scope := q.Scope
				var data any = struct{}{}
				if q.Action == "begin" {
					data = map[string]string{"step_id": "step"}
				}
				status := 200
				if req.URL.Path == "/bridge/before-generate" {
					if mode == "timeout" {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					if mode == "503" {
						status = 503
					}
					if mode == "scope" {
						scope.KnowledgeBaseID = "foreign"
					}
					text := "CURRENT_GUIDANCE"
					if mode == "oversize" {
						text = strings.Repeat("x", 801)
					}
					items := []map[string]any{{"id": "g", "revision": 1}}
					if mode == "duplicate" {
						items = append(items, items[0])
					}
					payload := map[string]any{"step_id": q.Guidance.StepID, "call_id": q.Guidance.CallID, "content": text, "items": items, "units": len(text) + 16}
					if mode == "annotation" || mode == "mixed" || mode == "bad-annotation" {
						const annotation = "APPROVED_CORRECTION"
						units := len(annotation) + 16
						if mode == "mixed" {
							units += len(text) + 16
						} else {
							payload["content"] = ""
						}
						if mode == "bad-annotation" {
							units++
						}
						payload["annotation_content"] = annotation
						payload["annotation_policy"] = "Use the approved factual correction when applicable."
						payload["units"] = units
					}
					if correction {
						if q.Guidance.Query != "QUESTION" || !q.Guidance.CorrectionEvidence {
							t.Fatal("the current question was not attested")
						}
						payload["content"], payload["units"], payload["items"] = "", 0, nil
						payload["annotation_policy"] = "TRUSTED_POLICY"
						payload["correction_content"] = "REVIEWED_EVIDENCE"
						payload["correction_items"] = []map[string]any{{"id": "c", "revision": 1, "document_id": "d", "version_id": "v", "chunk_id": "ch"}}
					}
					data = payload
				}
				body, _ := json.Marshal(map[string]any{"success": true, "request_id": q.RequestID, "scope": scope, "data": data})
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			messages := []chat.Message{{Role: "system", Content: "CACHED_PREFIX"}, {Role: "user", Content: "QUESTION"}}
			callCtx := ctx
			if mode == "unattributed" {
				callCtx = context.WithValue(ctx, sourceUnknownKey{}, true)
			}
			var inputs []SourceInput
			if correction {
				callCtx = WithCorrectionQuery(callCtx, "QUESTION")
				callCtx = WithCorrectionSourceHandles(callCtx, func(id string) string {
					if id == "ch" {
						return "chunk://1"
					}
					return ""
				})
				inputs = []SourceInput{{DocumentID: "d", ChunkID: "ch", Field: "content", ValueHash: SourceFieldHash("ORIGINAL_EVIDENCE")}}
			}
			if mode == "correction-after-tool" {
				messages = append(messages, chat.Message{Role: "tool", Content: "ORIGINAL_EVIDENCE"})
				callCtx = WithConsumedToolOutputs(callCtx, []ConsumedToolOutput{{StepID: "search", ToolCallID: "lookup", OutputHash: SourceFieldHash("ORIGINAL_EVIDENCE")}})
			}
			_, actual, call, err := BeginGuidedSourceCall(callCtx, messages, nil, inputs, mode != "unattributed")
			if err != nil {
				t.Fatal(err)
			}
			if messages[0].Content != "CACHED_PREFIX" || messages[1].Content != "QUESTION" {
				t.Fatal("original prompt mutated")
			}
			want := 2
			if mode == "valid" || mode == "unattributed" || mode == "mixed" {
				want = 3
			}
			system := actual[0].Content
			if correction {
				if system != "CACHED_PREFIX\n\nTRUSTED_POLICY" || len(call.CorrectionItems()) != 1 {
					t.Fatalf("trusted policy and verified correction receipt missing: %#v", actual)
				}
				last := actual[len(actual)-1]
				if last.Role != "user" || !strings.Contains(last.Content, "REVIEWED_EVIDENCE") || !strings.Contains(last.Content, "chunk://1") {
					t.Fatalf("reviewed evidence and original handle missing from final data: %#v", actual)
				}
				if mode == "correction-after-tool" {
					want = 4
					if actual[2].Role != "tool" || actual[2].Content != "ORIGINAL_EVIDENCE" {
						t.Fatalf("native tool result changed: %#v", actual)
					}
				} else if !strings.HasPrefix(last.Content, "QUESTION\n\n") {
					t.Fatalf("native user evidence changed: %#v", actual)
				}
			} else if mode == "annotation" || mode == "mixed" {
				if !strings.HasPrefix(system, "CACHED_PREFIX\n\n") || !strings.Contains(system, "APPROVED_CORRECTION") || !strings.Contains(system, "Use the approved factual correction when applicable.") || actual[1].Role != messages[1].Role || actual[1].Content != messages[1].Content {
					t.Fatalf("model did not get scoped instruction in initial system turn: %#v", actual)
				}
			} else if system != "CACHED_PREFIX" {
				t.Fatalf("mode=%s modified system context: %q", mode, system)
			}
			if len(actual) != want {
				t.Fatalf("mode=%s messages=%v", mode, actual)
			}
			// Qwen3.x chat templates reject any system message after the first one
			// ("System message must be at the beginning"). Guidance is delivered as a
			// trailing user turn so the request stays valid for every provider.
			for i := 1; i < len(actual); i++ {
				if actual[i].Role == "system" {
					t.Fatalf("mode=%s system message at index %d breaks strict chat templates", mode, i)
				}
			}
			if want == 3 {
				last := actual[len(actual)-1]
				if last.Role != "user" || last.Content != "CURRENT_GUIDANCE" {
					t.Fatalf("mode=%s guidance message role=%q content=%q", mode, last.Role, last.Content)
				}
			}
			encoded, _ := json.Marshal(struct{ Messages, Options any }{actual, (*chat.ChatOptions)(nil)})
			if call.inputHash != SourceHash(encoded) {
				t.Fatal("provenance does not hash actual injected request")
			}
		})
	}
}
