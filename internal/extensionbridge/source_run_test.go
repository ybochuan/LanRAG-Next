package extensionbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type provenanceRoundTrip func(*http.Request) (*http.Response, error)

func (f provenanceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSourceRunTransportBindsIdentityAndRejectsProtocolFailures(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("k", 32))
	original := sourceHTTPClient
	t.Cleanup(func() { sourceHTTPClient = original })
	identity := SourceRunIdentity{Scope: SourceScope{TenantID: 7, KnowledgeBaseID: "kb"}, UserID: "user", SessionID: "session", AttemptID: "attempt", AuthorizedDocumentIDs: []string{"doc"}}
	for _, mode := range []string{"valid", "trace", "scope", "owner", "unknown", "status", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			sourceHTTPClient = &http.Client{Timeout: 500 * time.Millisecond, Transport: provenanceRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "http://127.0.0.1:8090/provenance/runs/begin" || req.Header.Get("X-AgentRAG-Bridge-Version") != "1" || req.Header.Get("Authorization") != "Bearer "+strings.Repeat("k", 32) {
					t.Error("transport origin/credential/version")
				}
				if mode == "timeout" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				var body sourceRequest
				if e := json.NewDecoder(req.Body).Decode(&body); e != nil {
					t.Fatal(e)
				}
				h := SourceRunHandle{Scope: identity.Scope, RunID: strings.Repeat("a", 48), Capability: strings.Repeat("b", 64)}
				trace, scope := body.RequestID, identity.Scope
				if mode == "trace" {
					trace = "wrong"
				}
				if mode == "scope" {
					scope.TenantID = 8
				}
				if mode == "owner" {
					h.Scope.KnowledgeBaseID = "wrong"
				}
				envelope := map[string]any{"success": true, "request_id": trace, "scope": scope, "data": h}
				if mode == "unknown" {
					envelope["complete"] = true
				}
				encoded, _ := json.Marshal(envelope)
				status := 200
				if mode == "status" {
					status = 503
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(encoded))), Header: make(http.Header)}, nil
			})}
			ctx, e := BeginSourceRun(context.Background(), identity)
			if (e == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, e)
			}
			if mode == "valid" {
				if _, ok := SourceRunFromContext(ctx); !ok {
					t.Fatal("trusted handle lost")
				}
			}
		})
	}
}

func TestSourceCallRecordsActualPayloadAndMonotonicUnknown(t *testing.T) {
	t.Setenv("AGENTRAG_MANAGEMENT_KEY", strings.Repeat("k", 32))
	original := sourceHTTPClient
	t.Cleanup(func() { sourceHTTPClient = original })
	h := SourceRunHandle{Scope: SourceScope{TenantID: 7, KnowledgeBaseID: "kb"}, RunID: "run", Capability: strings.Repeat("b", 64)}
	ctx := context.WithValue(context.Background(), sourceRunKey{}, h)
	for _, complete := range []bool{true, false} {
		var requests []sourceRequest
		sourceHTTPClient = &http.Client{Transport: provenanceRoundTrip(func(req *http.Request) (*http.Response, error) {
			var q sourceRequest
			if err := json.NewDecoder(req.Body).Decode(&q); err != nil {
				t.Fatal(err)
			}
			requests = append(requests, q)
			data := any(struct{}{})
			if q.Action == "begin" {
				data = map[string]string{"step_id": "step"}
			}
			body, _ := json.Marshal(map[string]any{"success": true, "request_id": q.RequestID, "scope": q.Scope, "data": data})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})}
		payload := map[string]string{"messages": "ACTUAL_INPUT_CANARY"}
		_, call, err := BeginSourceCall(ctx, "model", payload, []SourceInput{{DocumentID: "doc", ChunkID: "chunk", Field: "content", ValueHash: SourceFieldHash("body")}}, complete)
		if err != nil {
			t.Fatal(err)
		}
		if !complete {
			if len(requests) != 2 || requests[1].Result.Outcome != "unknown" {
				t.Fatal("unknown composition was not committed before the model call")
			}
		} else {
			if len(requests) != 2 || requests[1].Input == nil {
				t.Fatal("actual field was not proved before model execution")
			}
		}
		call.Record(map[string]string{"content": "ACTUAL_OUTPUT_CANARY"})
		if err := call.Finish("success"); err != nil {
			t.Fatal(err)
		}
		last := requests[len(requests)-1]
		encoded, _ := json.Marshal(payload)
		if last.Result == nil || last.Result.InputHash != SourceHash(encoded) {
			t.Fatal("not bound to actual encoded request")
		}
		if complete {
			output, _ := json.Marshal(map[string]string{"content": "ACTUAL_OUTPUT_CANARY"})
			if last.Result.Outcome != "success" || last.Result.OutputHash != SourceHash(append(output, '\n')) {
				t.Fatal("not bound to actual output")
			}
		} else if len(requests) != 2 {
			t.Fatal("unknown call was washed to success")
		}
		for _, q := range requests {
			body, _ := json.Marshal(q)
			if strings.Contains(string(body), "CANARY") {
				t.Fatal("raw model data sent to Host")
			}
		}
	}
}

func TestSourceCallWithoutTrustedHandleDoesNotInventRun(t *testing.T) {
	_, call, err := BeginSourceCall(context.Background(), "model", "request", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	call.Record("response")
	if err := call.Finish("success"); err != nil {
		t.Fatal(err)
	}
}

func TestSourceCaptureRequiresAnAcknowledgedStep(t *testing.T) {
	ctx := context.WithValue(context.Background(), sourceRunKey{}, SourceRunHandle{Scope: SourceScope{TenantID: 7, KnowledgeBaseID: "kb"}, RunID: "run", Capability: strings.Repeat("a", 64)})
	if err := CaptureSourceInputs(ctx, nil, true); err == nil {
		t.Fatal("input captured without a trusted step")
	}
	if err := CaptureSourceInputs(context.Background(), nil, true); err != nil {
		t.Fatal("missing run created attribution")
	}
}
