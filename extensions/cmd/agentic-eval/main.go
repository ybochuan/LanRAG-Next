// Command agentic-eval runs a multi-hop question set against a live WeKnora Core
// and reports per-question turn counts, tool sequences and answer text.
//
// It is an evaluation client only: it creates chat sessions, asks questions and
// reads the server's own tool-call stream. It never writes governance state.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Question is one evaluation item. ExpectedDocuments lists the document IDs the
// answer should be grounded in; a manual review or a reviewer answer may be
// supplied because correctness is not machine-decidable from a tool sequence.
type Question struct {
	ID                string   `json:"id"`
	Question          string   `json:"question"`
	ExpectedDocuments []string `json:"expected_documents,omitempty"`
	ReviewerAnswer    string   `json:"reviewer_answer,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

// Manifest is the input file: a knowledge base and its multi-hop questions.
type Manifest struct {
	KnowledgeBaseID string     `json:"knowledge_base_id"`
	AgentID         string     `json:"agent_id"`
	ModelID         string     `json:"model_id,omitempty"`
	Questions       []Question `json:"questions"`
}

// ToolCall records one observed tool invocation from the server stream.
// Iteration is the zero-based ReAct round the server reported for the call.
type ToolCall struct {
	Name      string `json:"name"`
	Call      string `json:"call_id,omitempty"`
	Iteration int    `json:"iteration"`
	Round     int    `json:"round"`
}

// Result is one evaluated question.
type Result struct {
	ID            string     `json:"id"`
	Question      string     `json:"question"`
	SessionID     string     `json:"session_id,omitempty"`
	Answer        string     `json:"answer,omitempty"`
	Turns         int        `json:"turns"`
	Rounds        int        `json:"rounds"`
	ToolCalls     []ToolCall `json:"tool_calls"`
	ToolSequence  []string   `json:"tool_sequence"`
	DistinctTools int        `json:"distinct_tools"`
	Error         string     `json:"error,omitempty"`
	ElapsedMs     int64      `json:"elapsed_ms"`
}

// Report is the whole evaluation output.
type Report struct {
	GeneratedAt        string   `json:"generated_at"`
	CoreURL            string   `json:"core_url"`
	KnowledgeBase      string   `json:"knowledge_base_id"`
	AgentID            string   `json:"agent_id"`
	ModelID            string   `json:"model_id,omitempty"`
	QuestionCount      int      `json:"question_count"`
	MaxTurns           int      `json:"max_turns"`
	TurnLimit          int      `json:"turn_limit"`
	TurnLimitExceeded  int      `json:"turn_limit_exceeded"`
	TotalTurns         int      `json:"total_turns"`
	MaxTurnsUsed       int      `json:"max_turns_used"`
	TotalRounds        int      `json:"total_rounds"`
	MaxRoundsUsed      int      `json:"max_rounds_used"`
	RoundLimitExceeded int      `json:"round_limit_exceeded"`
	FailedCount        int      `json:"failed_count"`
	Results            []Result `json:"results"`
}

type streamFrame struct {
	ResponseType string          `json:"response_type"`
	Content      string          `json:"content"`
	Done         bool            `json:"done"`
	Data         json.RawMessage `json:"data"`
}

type toolFrame struct {
	ToolName   string `json:"tool_name"`
	ToolCallID string `json:"tool_call_id"`
	Iteration  int    `json:"iteration"`
	Round      int    `json:"round"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("agentic-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "evaluation manifest JSON")
	coreURL := flags.String("core", "http://127.0.0.1:18880", "live Core base URL")
	token := flags.String("token", "", "Core JWT")
	agentID := flags.String("agent", "", "agent id override")
	modelID := flags.String("model", "", "model id override")
	turnLimit := flags.Int("turn-limit", 4, "maximum agent turns per question")
	timeout := flags.Duration("timeout", 180*time.Second, "per-question timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return err
	}
	if *manifestPath == "" || *token == "" {
		return errors.New("manifest and token required")
	}
	if *turnLimit < 1 {
		return errors.New("turn-limit must be positive")
	}
	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		return errors.New("cannot read evaluation manifest")
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&manifest) != nil {
		return errors.New("invalid evaluation manifest")
	}
	if len(manifest.Questions) == 0 {
		return errors.New("evaluation manifest has no questions")
	}
	if *agentID != "" {
		manifest.AgentID = *agentID
	}
	if *modelID != "" {
		manifest.ModelID = *modelID
	}
	if manifest.AgentID == "" {
		return errors.New("manifest or agent flag must name the agent")
	}

	client := &http.Client{Timeout: *timeout}
	report := Report{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		CoreURL:       *coreURL,
		KnowledgeBase: manifest.KnowledgeBaseID,
		AgentID:       manifest.AgentID,
		ModelID:       manifest.ModelID,
		QuestionCount: len(manifest.Questions),
		MaxTurns:      *turnLimit,
		TurnLimit:     *turnLimit,
	}
	for _, question := range manifest.Questions {
		result := evaluate(ctx, client, *coreURL, *token, manifest, question, *turnLimit, *timeout)
		report.Results = append(report.Results, result)
		report.TotalTurns += result.Turns
		if result.Turns > report.MaxTurnsUsed {
			report.MaxTurnsUsed = result.Turns
		}
		if result.Turns > *turnLimit {
			report.TurnLimitExceeded++
		}
		report.TotalRounds += result.Rounds
		if result.Rounds > report.MaxRoundsUsed {
			report.MaxRoundsUsed = result.Rounds
		}
		if result.Rounds > *turnLimit {
			report.RoundLimitExceeded++
		}
		if result.Error != "" {
			report.FailedCount++
		}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// evaluate asks one question in its own session and reads the tool stream.
func evaluate(ctx context.Context, client *http.Client, coreURL, token string, manifest Manifest, question Question, turnLimit int, timeout time.Duration) Result {
	result := Result{ID: question.ID, Question: question.Question}
	started := time.Now()
	sessionID, err := createSession(ctx, client, coreURL, token, manifest)
	if err != nil {
		result.Error = "session creation failed: " + err.Error()
		result.ElapsedMs = time.Since(started).Milliseconds()
		return result
	}
	result.SessionID = sessionID
	answer, calls, err := askAgent(ctx, client, coreURL, token, manifest, sessionID, question.Question, timeout)
	result.ElapsedMs = time.Since(started).Milliseconds()
	result.Answer = answer
	result.ToolCalls = calls
	result.ToolSequence = make([]string, 0, len(calls))
	distinct := map[string]struct{}{}
	for _, call := range calls {
		result.ToolSequence = append(result.ToolSequence, call.Name)
		distinct[call.Name] = struct{}{}
	}
	result.DistinctTools = len(distinct)
	result.Turns = len(calls)
	result.Rounds = roundsUsed(calls)
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

// roundsUsed reports the number of distinct ReAct rounds the server used.
// A round is the server's own iteration counter, so parallel tool calls inside
// one round count once. Rounds without a reported counter are counted as
// separate rounds because the server did not label them.
func roundsUsed(calls []ToolCall) int {
	if len(calls) == 0 {
		return 0
	}
	seen := map[int]struct{}{}
	unlabelled := 0
	for _, call := range calls {
		if call.Iteration < 0 {
			unlabelled++
			continue
		}
		seen[call.Iteration] = struct{}{}
	}
	return len(seen) + unlabelled
}

func createSession(ctx context.Context, client *http.Client, coreURL, token string, manifest Manifest) (string, error) {
	body, err := json.Marshal(map[string]any{
		"title":              "agentic-eval " + time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"knowledge_base_ids": []string{manifest.KnowledgeBaseID},
		"agent_id":           manifest.AgentID,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, coreURL+"/api/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("core returned HTTP %d", response.StatusCode)
	}
	var decoded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", errors.New("core returned an unreadable session")
	}
	if decoded.Data.ID == "" {
		return "", errors.New("core returned no session id")
	}
	return decoded.Data.ID, nil
}

// askAgent streams one question and returns the answer plus every tool call.
func askAgent(ctx context.Context, client *http.Client, coreURL, token string, manifest Manifest, sessionID, query string, timeout time.Duration) (string, []ToolCall, error) {
	payload := map[string]any{
		"query":              query,
		"agent_id":           manifest.AgentID,
		"knowledge_base_ids": []string{manifest.KnowledgeBaseID},
	}
	if manifest.ModelID != "" {
		payload["model_id"] = manifest.ModelID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	// A zero timeout means no deadline: an evaluation request must not be
	// cut off by a default that the caller never chose.
	streamCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	request, err := http.NewRequestWithContext(streamCtx, http.MethodPost, coreURL+"/api/v1/agent-chat/"+sessionID, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("core returned HTTP %d", response.StatusCode)
	}
	var answer strings.Builder
	var calls []ToolCall
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		var frame streamFrame
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			continue
		}
		switch frame.ResponseType {
		case "answer":
			answer.WriteString(frame.Content)
		case "tool_call":
			var tool toolFrame
			if len(frame.Data) > 0 && json.Unmarshal(frame.Data, &tool) == nil && tool.ToolName != "" {
				calls = append(calls, ToolCall{Name: tool.ToolName, Call: tool.ToolCallID, Iteration: tool.Iteration, Round: tool.Round})
			}
		case "error":
			return answer.String(), calls, errors.New("core reported an error frame")
		}
	}
	if err := scanner.Err(); err != nil {
		return answer.String(), calls, err
	}
	if answer.Len() == 0 && len(calls) == 0 {
		return "", calls, errors.New("core returned no answer and no tool call")
	}
	return answer.String(), calls, nil
}
