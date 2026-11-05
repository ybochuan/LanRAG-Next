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
	for _, mode := range []string{"valid", "unattributed", "503", "timeout", "scope", "oversize", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
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
					data = map[string]any{"step_id": q.Guidance.StepID, "call_id": q.Guidance.CallID, "content": text, "items": items, "units": len(text) + 16}
				}
				body, _ := json.Marshal(map[string]any{"success": true, "request_id": q.RequestID, "scope": scope, "data": data})
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			messages := []chat.Message{{Role: "system", Content: "CACHED_PREFIX"}, {Role: "user", Content: "QUESTION"}}
			callCtx := ctx
			if mode == "unattributed" {
				callCtx = context.WithValue(ctx, sourceUnknownKey{}, true)
			}
			_, actual, call, err := BeginGuidedSourceCall(callCtx, messages, nil, nil, mode != "unattributed")
			if err != nil {
				t.Fatal(err)
			}
			if messages[0].Content != "CACHED_PREFIX" || len(messages) != 2 {
				t.Fatal("original prompt mutated")
			}
			want := 2
			if mode == "valid" || mode == "unattributed" {
				want = 3
			}
			if len(actual) != want {
				t.Fatalf("mode=%s messages=%v", mode, actual)
			}
			encoded, _ := json.Marshal(struct{ Messages, Options any }{actual, (*chat.ChatOptions)(nil)})
			if call.inputHash != SourceHash(encoded) {
				t.Fatal("provenance does not hash actual injected request")
			}
		})
	}
}
