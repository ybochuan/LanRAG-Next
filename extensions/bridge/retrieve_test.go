package bridge_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/bridge"
	"github.com/ybochuan/LanRAG-Next/extensions/contract"
)

type policyFunc func(context.Context, contract.BeforeRetrieveRequest) ([]string, error)

func (f policyFunc) ExcludedDocumentIDs(ctx context.Context, req contract.BeforeRetrieveRequest) ([]string, error) {
	return f(ctx, req)
}

func TestDefaultPassThroughKeepsNativeAuthorizedCandidates(t *testing.T) {
	s := httptest.NewServer(bridge.NewServerWithServices(bridge.Services{BeforeRetrieve: bridge.NewPassThroughRetrieveHandler()}).Handler)
	t.Cleanup(s.Close)
	base := retrieveFixture(t, "request")
	for _, input := range []string{base, strings.ReplaceAll(strings.Replace(base, `"tenant_id": 7`, `"tenant_id": 8`, 1), "kb-a", "kb-other")} {
		status, body := postRetrieve(t, s, "/bridge/before-retrieve", input, "1")
		var request contract.BeforeRetrieveRequest
		var got contract.BeforeRetrieveResponse
		if err := json.Unmarshal([]byte(input), &request); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if status != 200 || got.TenantID != request.TenantID || got.RequestID != request.RequestID || !reflect.DeepEqual(got.KnowledgeBaseIDs, request.KnowledgeBaseIDs) || !reflect.DeepEqual(got.KnowledgeIDs, []string{"doc-a", "doc-b", "doc-c"}) || got.DenyAll || got.ExcludedDocumentIDs == nil || len(got.ExcludedDocumentIDs) != 0 {
			t.Fatalf("pass-through changed the request scope: status=%d response=%+v", status, got)
		}
	}
	for _, invalid := range []string{"{", strings.Replace(base, `"knowledge_ids": ["doc-a", "doc-b", "doc-c"]`, `"knowledge_ids": null`, 1)} {
		status, _ := postRetrieve(t, s, "/bridge/before-retrieve", invalid, "1")
		if status != 400 {
			t.Fatalf("invalid pass-through input accepted: %d", status)
		}
	}
	status, _ := postRetrieve(t, s, "/bridge/before-retrieve", retrieveFixture(t, "empty"), "1")
	if status != 403 {
		t.Fatalf("status-only native gate must reject an empty document scope: %d", status)
	}
	status, _ = postRetrieve(t, s, "/bridge/before-retrieve", base, "2")
	if status != 400 {
		t.Fatal("pass-through bypassed version gate")
	}
}

func retrieveFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../contracts/bridge/v1/before-retrieve." + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func retrieveServer(t *testing.T, policy policyFunc) *httptest.Server {
	t.Helper()
	h, err := bridge.NewRetrieveHandler(7, []string{"kb-a", "kb-b"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(bridge.NewServerWithServices(bridge.Services{BeforeRetrieve: h}).Handler)
	t.Cleanup(s.Close)
	return s
}

func postRetrieve(t *testing.T, s *httptest.Server, path, body, version string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, s.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if version != "" {
		r.Header.Set(bridge.VersionHeader, version)
	}
	response, err := s.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get(bridge.VersionHeader) != "1" {
		t.Fatal("supported version absent")
	}
	return response.StatusCode, b
}

func TestRetrieveNarrowsScopeAndPreservesPublicContext(t *testing.T) {
	var calls atomic.Int32
	s := retrieveServer(t, func(ctx context.Context, req contract.BeforeRetrieveRequest) ([]string, error) {
		calls.Add(1)
		if ctx.Err() != nil || req.TenantID != 7 || req.RequestID != "request-1" || req.SessionID != "session-1" || req.Mode != "agent" || req.AgentRound != 2 || req.TokenBudget != 800 || req.OriginalQuery != "source query" || req.RewrittenQuery != "rewritten query" || !reflect.DeepEqual(req.KnowledgeBaseIDs, []string{"kb-a", "kb-b"}) || !reflect.DeepEqual(req.KnowledgeIDs, []string{"doc-a", "doc-b", "doc-c"}) || !reflect.DeepEqual(req.TagScopes, map[string][]string{"kb-a": {"tag-business"}}) || !strings.Contains(string(req.Metadata["trace"]), "9007199254740993") {
			t.Errorf("policy context lost: %+v", req)
		}
		return []string{"doc-b"}, nil
	})
	status, body := postRetrieve(t, s, "/bridge/before-retrieve", retrieveFixture(t, "request"), "1")
	if status != 200 || calls.Load() != 1 {
		t.Fatalf("status=%d calls=%d body=%s", status, calls.Load(), body)
	}
	var got, want contract.BeforeRetrieveResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(retrieveFixture(t, "response")), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for _, path := range []string{"/bridge/after-retrieve", "/bridge/before-generate", "/bridge/ingest-event"} {
		status, _ := postRetrieve(t, s, path, `{}`, "1")
		if status != 503 {
			t.Fatalf("unconfigured %s returned %d", path, status)
		}
	}
	resp, err := s.Client().Get(s.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal("partial handler must not mark Host ready")
	}
}

func TestRetrieveEmptyAndFullyExcludedNeverBecomeWholeKB(t *testing.T) {
	for _, emptyInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "all_excluded", true: "explicit_empty"}[emptyInput], func(t *testing.T) {
			var calls atomic.Int32
			s := retrieveServer(t, func(context.Context, contract.BeforeRetrieveRequest) ([]string, error) {
				calls.Add(1)
				return []string{"doc-a", "doc-b", "doc-c"}, nil
			})
			input := retrieveFixture(t, "request")
			if emptyInput {
				input = retrieveFixture(t, "empty")
			}
			status, body := postRetrieve(t, s, "/bridge/before-retrieve", input, "1")
			var got contract.BeforeRetrieveResponse
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			if status != 200 || !got.DenyAll || got.KnowledgeIDs == nil || len(got.KnowledgeIDs) != 0 || (emptyInput && calls.Load() != 0) || (!emptyInput && calls.Load() != 1) {
				t.Fatalf("unsafe empty scope: %d %+v calls=%d", status, got, calls.Load())
			}
		})
	}
}

func TestRetrieveRejectsInvalidBodyAndScopeBeforePolicy(t *testing.T) {
	var calls atomic.Int32
	s := retrieveServer(t, func(context.Context, contract.BeforeRetrieveRequest) ([]string, error) { calls.Add(1); return nil, nil })
	base := retrieveFixture(t, "request")
	for _, tc := range []struct {
		name, body, version string
		status              int
	}{
		{"malformed", "{", "1", 400},
		{"null_scope", strings.Replace(base, `["doc-a", "doc-b", "doc-c"]`, "null", 1), "1", 400},
		{"duplicate", strings.Replace(base, `"tenant_id": 7`, `"tenant_id":8,"tenant_id":7`, 1), "1", 400},
		{"unknown", strings.Replace(base, `"mode":`, `"extra":true,"mode":`, 1), "1", 400},
		{"alias", strings.Replace(base, `"tenant_id"`, `"TENANT_ID"`, 1), "1", 400},
		{"trailing", base + `{}`, "1", 400},
		{"oversized", base + strings.Repeat(" ", contract.MaxRequestBytes), "1", 413},
		{"foreign_tenant", strings.Replace(base, `"tenant_id": 7`, `"tenant_id":8`, 1), "1", 403},
		{"foreign_kb", strings.ReplaceAll(base, "kb-b", "kb-foreign"), "1", 403},
		{"foreign_tag", strings.Replace(base, `"tag_scopes": {"kb-a"`, `"tag_scopes": {"kb-foreign"`, 1), "1", 400},
		{"missing_version", base, "", 400},
		{"new_version", base, "2", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRetrieve(t, s, "/bridge/before-retrieve", tc.body, tc.version)
			if status != tc.status || calls.Load() != 0 || strings.Contains(string(body), "source query") {
				t.Fatalf("status=%d want=%d calls=%d body=%s", status, tc.status, calls.Load(), body)
			}
		})
	}
}

func TestRetrievePolicyFailuresCannotPermitOrLeak(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		err  error
	}{
		{"error", nil, errors.New("secret backend details")},
		{"foreign_id", []string{"outside"}, nil},
		{"duplicate", []string{"doc-b", "doc-b"}, nil},
		{"empty_id", []string{""}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := retrieveServer(t, func(context.Context, contract.BeforeRetrieveRequest) ([]string, error) { return tc.ids, tc.err })
			status, body := postRetrieve(t, s, "/bridge/before-retrieve", retrieveFixture(t, "request"), "1")
			if status != 503 || strings.Contains(string(body), "secret") || strings.Contains(string(body), "knowledge_ids") {
				t.Fatalf("unsafe failure response: %d %s", status, body)
			}
		})
	}
}

func TestRetrieveCancellationNeverReturnsPermit(t *testing.T) {
	for _, before := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var calls int
		h, err := bridge.NewRetrieveHandler(7, []string{"kb-a", "kb-b"}, policyFunc(func(inner context.Context, _ contract.BeforeRetrieveRequest) ([]string, error) {
			calls++
			cancel()
			if inner.Err() != context.Canceled {
				t.Error("request context not propagated")
			}
			return nil, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if before {
			cancel()
		}
		r := httptest.NewRequest(http.MethodPost, "/bridge/before-retrieve", strings.NewReader(retrieveFixture(t, "request"))).WithContext(ctx)
		r.Header.Set(bridge.VersionHeader, "1")
		w := httptest.NewRecorder()
		bridge.NewServerWithServices(bridge.Services{BeforeRetrieve: h}).Handler.ServeHTTP(w, r)
		cancel()
		if w.Code != 503 || (before && calls != 0) || (!before && calls != 1) {
			t.Fatalf("canceled request permitted: %d calls=%d", w.Code, calls)
		}
	}
}

func TestRetrieveOwnsConfiguredAndCandidateScope(t *testing.T) {
	kbs := []string{"kb-a", "kb-b"}
	h, err := bridge.NewRetrieveHandler(7, kbs, policyFunc(func(_ context.Context, r contract.BeforeRetrieveRequest) ([]string, error) {
		r.KnowledgeIDs[0] = "injected"
		r.KnowledgeBaseIDs[0] = "injected-kb"
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	kbs[0] = "mutated-config"
	s := httptest.NewServer(bridge.NewServerWithServices(bridge.Services{BeforeRetrieve: h}).Handler)
	t.Cleanup(s.Close)
	status, body := postRetrieve(t, s, "/bridge/before-retrieve", retrieveFixture(t, "request"), "1")
	var got contract.BeforeRetrieveResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if status != 200 || !reflect.DeepEqual(got.KnowledgeIDs, []string{"doc-a", "doc-b", "doc-c"}) || !reflect.DeepEqual(got.KnowledgeBaseIDs, []string{"kb-a", "kb-b"}) {
		t.Fatalf("mutable input expanded scope: %d %+v", status, got)
	}
}

func TestRetrieveRejectsInvalidConfiguration(t *testing.T) {
	policy := policyFunc(func(context.Context, contract.BeforeRetrieveRequest) ([]string, error) { return nil, nil })
	for _, tc := range []struct {
		tenant uint64
		kbs    []string
		policy bridge.RetrievePolicy
	}{
		{0, []string{"kb-a"}, policy}, {7, nil, policy}, {7, []string{}, policy}, {7, []string{"kb-a", "kb-a"}, policy}, {7, []string{"../kb"}, policy}, {7, []string{"kb-a"}, nil},
	} {
		if _, err := bridge.NewRetrieveHandler(tc.tenant, tc.kbs, tc.policy); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestConfiguredBridgeStopsStalledRequestBodyBeforePolicy(t *testing.T) {
	var calls atomic.Int32
	h, err := bridge.NewRetrieveHandler(7, []string{"kb-a", "kb-b"}, policyFunc(func(context.Context, contract.BeforeRetrieveRequest) ([]string, error) {
		calls.Add(1)
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := bridge.NewServerWithServices(bridge.Services{BeforeRetrieve: h})
	s := httptest.NewUnstartedServer(cfg.Handler)
	s.Config = cfg
	s.Start()
	t.Cleanup(s.Close)
	conn, err := net.DialTimeout("tcp", s.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(7 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(conn, "POST /bridge/before-retrieve HTTP/1.1\r\nHost: localhost\r\n%s: 1\r\nContent-Length: 100\r\n\r\n{", bridge.VersionHeader)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("stalled body did not receive a bounded rejection: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != 400 || calls.Load() != 0 {
		t.Fatalf("incomplete body reached policy: status=%d calls=%d", response.StatusCode, calls.Load())
	}
	status, _ := postRetrieve(t, s, "/bridge/before-retrieve", retrieveFixture(t, "request"), "1")
	if status != 200 || calls.Load() != 1 {
		t.Fatal("server failed to recover after stalled body")
	}
}
