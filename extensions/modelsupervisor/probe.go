package modelsupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Probe outcomes that are worth telling the operator apart from a plain failure.
const (
	probeNoteNotListening   = "no process is listening on the configured port"
	probeNoteHealthFailed   = "the health endpoint did not answer 200"
	probeNoteTaskFailed     = "the task endpoint rejected the probe request"
	probeNoteWSLUnavailable = "cross-OS probe unavailable"
	probeNoteWSLBlocked     = "reachable from Windows but not from the WSL side"
	probeNoteReady          = "reachable from both sides"
)

// ProbeResult is what /probe and the start path report about one instance. It
// deliberately answers "does this address actually work from where the core
// runs" rather than "did the process start", because those two diverge exactly
// in the failure mode this manager exists to prevent.
type ProbeResult struct {
	Reachability  Reachability `json:"reachability"`
	EmbeddingDim  int          `json:"embedding_dim,omitempty"`
	LatencyMS     int64        `json:"health_latency_ms,omitempty"`
	Note          string       `json:"note,omitempty"`
	ObservedPorts []int        `json:"observed_ports,omitempty"`
}

// Prober checks an instance from the host and from the WSL side. Every external
// dependency is a field so tests never need a real llama-server or a real WSL.
type Prober struct {
	Client      *http.Client
	DialTimeout time.Duration
	WSLDistro   string
	WSLProbe    func(ctx context.Context, url string) (int, error)
	Dial        func(addr string) error
}

func (p *Prober) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 3 * time.Second}
}

// taskClient is the client used for the task request. The budget comes from the
// task type: chat spends seconds on CPU prompt processing and embedding pays a
// one-off cost the first time a request runs through the graph, while a rerank
// server answers immediately and should be reported as wedged straight away.
func (p *Prober) taskClient(t TaskType) *http.Client {
	clone := *p.client()
	clone.Timeout = t.taskTimeout()
	return &clone
}

func (p *Prober) dial() func(addr string) error {
	if p.Dial != nil {
		return p.Dial
	}
	return func(addr string) error {
		timeout := p.DialTimeout
		if timeout <= 0 {
			timeout = time.Second
		}
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

func (p *Prober) wsl() func(ctx context.Context, url string) (int, error) {
	if p.WSLProbe != nil {
		return p.WSLProbe
	}
	distro := p.WSLDistro
	return func(ctx context.Context, url string) (int, error) {
		args := []string{}
		if distro != "" {
			args = append(args, "-d", distro)
		}
		args = append(args, "-e", "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "3", url)
		out, err := exec.CommandContext(ctx, "wsl.exe", args...).Output()
		if err != nil {
			return 0, err
		}
		return strconv.Atoi(strings.TrimSpace(string(out)))
	}
}

// Check runs the four layers in order and reports the strongest conclusion the
// evidence supports. A probe that cannot be completed degrades to unverified; it
// never upgrades to verified without a positive answer.
func (p *Prober) Check(ctx context.Context, spec Spec) ProbeResult {
	spec.Normalize()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(spec.Port))
	result := ProbeResult{Reachability: ReachUnverified}

	if err := p.dial()(addr); err != nil {
		result.Note = probeNoteNotListening + ": " + err.Error()
		return result
	}

	started := time.Now()
	status, err := p.get(ctx, "http://"+addr+"/health")
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Note = probeNoteHealthFailed + ": " + err.Error()
		return result
	}
	if status != http.StatusOK {
		result.Note = fmt.Sprintf("%s: HTTP %d", probeNoteHealthFailed, status)
		return result
	}

	dim, err := p.task(ctx, spec)
	if err != nil {
		result.Note = probeNoteTaskFailed + ": " + err.Error()
		return result
	}
	result.EmbeddingDim = dim

	code, err := p.wsl()(ctx, "http://"+addr+"/health")
	switch {
	case err != nil:
		result.Note = probeNoteWSLUnavailable + ": " + err.Error()
	case code == http.StatusOK:
		result.Reachability = ReachLoopback
		result.Note = probeNoteReady
	default:
		result.Note = fmt.Sprintf("%s: HTTP %d", probeNoteWSLBlocked, code)
	}
	return result
}

func (p *Prober) get(ctx context.Context, url string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	response, err := p.client().Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	return response.StatusCode, nil
}

// task exercises the endpoint the core actually calls. A 200 on /health only
// proves the process is up; it does not prove the task flag was passed, so an
// embedding instance without --embedding would look healthy and then fail every
// real request.
func (p *Prober) task(ctx context.Context, spec Spec) (int, error) {
	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(spec.Port))
	var payload any
	switch spec.TaskType {
	case TaskEmbedding:
		payload = map[string]any{"input": "ping"}
	case TaskRerank:
		payload = map[string]any{"query": "ping", "documents": []string{"alpha", "beta"}, "top_n": 1}
	case TaskChat:
		// One token is enough to prove the chat template loads and the model
		// answers; asking for more would make a health probe cost real GPU time.
		payload = map[string]any{
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
			"max_tokens": 1,
			"stream":     false,
		}
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnknownTask, spec.TaskType)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+spec.TaskType.probePath(), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.taskClient(spec.TaskType).Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", response.StatusCode, truncate(string(data), 200))
	}
	if spec.TaskType == TaskEmbedding {
		var decoded struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return 0, fmt.Errorf("decode embeddings: %w", err)
		}
		if len(decoded.Data) == 0 {
			return 0, errors.New("embedding response carried no data")
		}
		return len(decoded.Data[0].Embedding), nil
	}
	if spec.TaskType == TaskChat {
		// A completion with no choices means the request was accepted by the HTTP
		// layer but not served, which is exactly the "wrong mode flag" shape.
		var decoded struct {
			Choices []json.RawMessage `json:"choices"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return 0, fmt.Errorf("decode completion: %w", err)
		}
		if len(decoded.Choices) == 0 {
			return 0, errors.New("completion response carried no choices")
		}
		return 0, nil
	}
	var decoded struct {
		Results []struct {
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return 0, fmt.Errorf("decode rerank: %w", err)
	}
	if len(decoded.Results) == 0 {
		return 0, errors.New("rerank response carried no results")
	}
	return 0, nil
}

// ObservePorts reports which of the candidate ports currently accept a TCP
// connection. It exists to answer "the core points at 8081 but nothing answers
// there" without reading the core database, which is not this module's fact.
func ObservePorts(ctx context.Context, ports []int, dial func(addr string) error) []int {
	if dial == nil {
		dial = func(addr string) error {
			conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
			if err != nil {
				return err
			}
			return conn.Close()
		}
	}
	observed := make([]int, 0, len(ports))
	for _, port := range ports {
		if err := ctx.Err(); err != nil {
			return observed
		}
		if dial(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) == nil {
			observed = append(observed, port)
		}
	}
	return observed
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
