package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRoundsUsedCountsDistinctIterations(t *testing.T) {
	calls := []ToolCall{
		{Name: "search_knowledge", Iteration: 0},
		{Name: "read_document", Iteration: 0},
		{Name: "read_document", Iteration: 1},
		{Name: "list_documents", Iteration: 2},
		{Name: "read_document", Iteration: 2},
	}
	if got := roundsUsed(calls); got != 3 {
		t.Fatalf("roundsUsed = %d, want 3 distinct iterations", got)
	}
}

func TestRoundsUsedEmpty(t *testing.T) {
	if got := roundsUsed(nil); got != 0 {
		t.Fatalf("roundsUsed(nil) = %d, want 0", got)
	}
}

// Unlabelled calls are counted separately: the server reported no
// iteration for them, so they cannot be folded into a known round.
func TestRoundsUsedCountsUnlabelledSeparately(t *testing.T) {
	calls := []ToolCall{
		{Name: "search_knowledge", Iteration: 0},
		{Name: "read_document", Iteration: -1},
		{Name: "read_document", Iteration: -1},
	}
	if got := roundsUsed(calls); got != 3 {
		t.Fatalf("roundsUsed = %d, want 1 labelled round plus 2 unlabelled", got)
	}
}

func TestAskAgentParsesFramesAndToolSequence(t *testing.T) {
	frames := []string{
		`{"response_type":"agent_query","content":"","done":true,"data":{}}`,
		`{"response_type":"answer","content":"A","done":false,"data":{}}`,
		`{"response_type":"tool_call","content":"Calling tool: search_knowledge","done":false,"data":{"tool_name":"search_knowledge","tool_call_id":"c1","iteration":0,"round":1}}`,
		`{"response_type":"tool_call","content":"Calling tool: read_document","done":false,"data":{"tool_name":"read_document","tool_call_id":"c2","iteration":1,"round":2}}`,
		`{"response_type":"answer","content":"B","done":false,"data":{}}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("event:message\ndata:" + frame + "\n\n")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body.String()))
	}))
	defer server.Close()

	manifest := Manifest{KnowledgeBaseID: "kb-1", AgentID: "agent-1"}
	answer, calls, err := askAgent(context.Background(), server.Client(), server.URL, "token", manifest, "sess-1", "q", 10*time.Second)
	if err != nil {
		t.Fatalf("askAgent returned error: %v", err)
	}
	if answer != "AB" {
		t.Fatalf("answer = %q, want %q", answer, "AB")
	}
	if len(calls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(calls))
	}
	if calls[0].Name != "search_knowledge" || calls[0].Round != 1 {
		t.Fatalf("first call = %+v, want search_knowledge round 1", calls[0])
	}
	if calls[1].Iteration != 1 || calls[1].Round != 2 {
		t.Fatalf("second call = %+v, want iteration 1 round 2", calls[1])
	}
}

func TestAskAgentReportsErrorFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event:message\ndata:{\"response_type\":\"error\",\"content\":\"boom\"}\n\n"))
	}))
	defer server.Close()

	manifest := Manifest{KnowledgeBaseID: "kb-1", AgentID: "agent-1"}
	_, _, err := askAgent(context.Background(), server.Client(), server.URL, "token", manifest, "sess-1", "q", 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "error frame") {
		t.Fatalf("err = %v, want an error frame failure", err)
	}
}

func TestAskAgentRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	manifest := Manifest{KnowledgeBaseID: "kb-1", AgentID: "agent-1"}
	_, _, err := askAgent(context.Background(), server.Client(), server.URL, "token", manifest, "sess-1", "q", 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want an HTTP status failure", err)
	}
}

func TestCreateSessionReadsDataID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "sess-42"}})
	}))
	defer server.Close()

	id, err := createSession(context.Background(), server.Client(), server.URL, "token", Manifest{KnowledgeBaseID: "kb-1"})
	if err != nil {
		t.Fatalf("createSession returned error: %v", err)
	}
	if id != "sess-42" {
		t.Fatalf("session id = %q, want sess-42", id)
	}
}

func TestCreateSessionRejectsMissingID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	defer server.Close()

	if _, err := createSession(context.Background(), server.Client(), server.URL, "token", Manifest{KnowledgeBaseID: "kb-1"}); err == nil {
		t.Fatal("createSession accepted a response without a session id")
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("cannot write manifest: %v", err)
	}
	return path
}

func TestRunRejectsManifestWithoutQuestions(t *testing.T) {
	path := writeManifest(t, `{"knowledge_base_id":"kb-1","agent_id":"a","questions":[]}`)
	err := run(context.Background(), []string{"--manifest", path, "--token", "t"}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "no questions") {
		t.Fatalf("err = %v, want a no-questions failure", err)
	}
}

func TestRunRequiresToken(t *testing.T) {
	path := writeManifest(t, `{"knowledge_base_id":"kb-1","agent_id":"a","questions":[{"id":"Q1","question":"q"}]}`)
	err := run(context.Background(), []string{"--manifest", path}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v, want a missing-token failure", err)
	}
}

func TestRunRejectsUnknownManifestFields(t *testing.T) {
	path := writeManifest(t, `{"knowledge_base_id":"kb-1","agent_id":"a","unexpected":1,"questions":[{"id":"Q1","question":"q"}]}`)
	err := run(context.Background(), []string{"--manifest", path, "--token", "t"}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "invalid evaluation manifest") {
		t.Fatalf("err = %v, want an invalid-manifest failure", err)
	}
}

func TestRunRejectsNegativeTurnLimit(t *testing.T) {
	path := writeManifest(t, `{"knowledge_base_id":"kb-1","agent_id":"a","questions":[{"id":"Q1","question":"q"}]}`)
	err := run(context.Background(), []string{"--manifest", path, "--token", "t", "--turn-limit", "0"}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "turn-limit") {
		t.Fatalf("err = %v, want a turn-limit failure", err)
	}
}

func TestRunReportsHTTPFailureAsQuestionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "sess-1"}})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	path := writeManifest(t, `{"knowledge_base_id":"kb-1","agent_id":"a","questions":[{"id":"Q1","question":"q"}]}`)
	var out strings.Builder
	if err := run(context.Background(), []string{"--manifest", path, "--token", "t", "--core", server.URL}, &out); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	var report Report
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if report.FailedCount != 1 {
		t.Fatalf("failed count = %d, want 1", report.FailedCount)
	}
	if report.Results[0].Error == "" {
		t.Fatal("expected the failed question to carry an error")
	}
}
