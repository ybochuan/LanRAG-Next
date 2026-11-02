// Package modelsupervisor owns the lifecycle of local llama-server processes
// that serve the AgentRAG model engine. It is a local operations component: it
// never writes governance state, never reads the native database and never
// participates in retrieval decisions.
package modelsupervisor

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// TaskType selects which llama-server serving mode an instance runs in. The
// value is not cosmetic: for the retrieval tasks llama-server starts without a
// task flag and answers /health, yet rejects every task request, so the flag
// must follow the type. Chat needs no flag — it is the default mode.
type TaskType string

// Supported task types.
const (
	TaskEmbedding TaskType = "embedding"
	TaskRerank    TaskType = "rerank"
	TaskChat      TaskType = "chat"
)

// Valid reports whether the task type is one this package can serve.
func (t TaskType) Valid() bool {
	return t == TaskEmbedding || t == TaskRerank || t == TaskChat
}

// flag is the llama-server switch that enables the serving mode. Chat returns
// the empty string: it is what llama-server does when no mode flag is given, and
// passing an unknown flag would make the process exit at startup.
func (t TaskType) flag() string {
	switch t {
	case TaskRerank:
		return "--rerank"
	case TaskChat:
		return ""
	default:
		return "--embedding"
	}
}

// probePath is the OpenAI-compatible route that exercises the serving mode. The
// plural spelling matters: llama-server exposes /v1/embeddings and /v1/rerank.
func (t TaskType) probePath() string {
	switch t {
	case TaskRerank:
		return "/v1/rerank"
	case TaskChat:
		return "/v1/chat/completions"
	default:
		return "/v1/embeddings"
	}
}

// taskTimeout bounds the task probe. A warm retrieval model answers in
// milliseconds; a chat model on CPU spends seconds on prompt processing alone,
// so sharing one health timeout would report a working server as broken.
//
// Embedding keeps a budget of its own because llama.cpp pays a one-off cost the
// first time a request runs through the graph: measured on this host, the first
// call after load took 12.3s, the second 5.0s, the third 0.10s. A 3s budget
// declared a loaded, healthy server unreachable for the first minute.
func (t TaskType) taskTimeout() time.Duration {
	switch t {
	case TaskChat:
		return 45 * time.Second
	case TaskEmbedding:
		return 20 * time.Second
	default:
		return 3 * time.Second
	}
}

// Status is the observed lifecycle state of one instance.
type Status string

// Lifecycle states. Transitions are restricted; see Manager.transition.
const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusFailed   Status = "failed"
)

// Reachability records how far an endpoint address was verified. It is an
// observation, never a guess: unverified means the cross-OS probe could not run,
// not that the endpoint is broken.
type Reachability string

// Reachability values.
const (
	ReachLoopback   Reachability = "loopback"
	ReachGateway    Reachability = "gateway"
	ReachUnverified Reachability = "unverified"
)

// Address is the base URL a caller configures in the core model settings.
//
// It carries no reachability: whether that address works from where the core
// runs is an observation, and an observation recorded at start time is wrong the
// moment the network changes. Reachability is reported by /probe and by Health,
// both of which say when they were measured.
type Address struct {
	URL string `json:"url"`
}

// Default ports deliberately match the endpoints the native core already has
// configured. Changing them would silently break every existing installation.
// Chat has no such precedent, so 8083 is a free port next to the other two.
const (
	DefaultEmbeddingPort = 8081
	DefaultRerankPort    = 8082
	DefaultChatPort      = 8083
	DefaultCtxSize       = 4096
	DefaultThreads       = 8
)

// reservedPorts belong to other AgentRAG components and must never be claimed
// by a model instance.
var reservedPorts = map[int]bool{8000: true, 8080: true, 8090: true, 8091: true, 18880: true}

// idPattern keeps identifiers safe for use as a llama-server alias and as a
// JSON/log field without escaping.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Validation failures. Callers surface these as 400 responses.
var (
	ErrInvalidID        = errors.New("instance id must be lowercase alphanumeric with dashes")
	ErrUnknownTask      = errors.New("unknown task type")
	ErrInvalidModel     = errors.New("model must name a .gguf file")
	ErrInvalidPort      = errors.New("port must be 1024-65535 and not reserved")
	ErrInvalidCtxSize   = errors.New("ctx_size must be 0 or 512-131072")
	ErrInvalidThreads   = errors.New("threads must be 0 or 1-256")
	ErrInvalidGPULayers = errors.New("gpu_layers must be 0-1024")
	ErrConflictingGPU   = errors.New("use_gpu and gpu_layers disagree")
	ErrDuplicatePort    = errors.New("port already claimed by another instance")
	ErrPortInUse        = errors.New("port is already in use on this host")
	ErrDuplicateID      = errors.New("instance id already exists")
	ErrPathEscape       = errors.New("path escapes its configured root")
	ErrReservedArg      = errors.New("extra_args must not set a supervisor-owned flag")
	ErrInvalidArg       = errors.New("extra_args entries must be non-empty flags")
	ErrUnknownInstance  = errors.New("unknown instance")
	ErrStateConflict    = errors.New("state transition not allowed")
	ErrResourceDenied   = errors.New("resource precheck failed")
)

// Spec is the persisted intent for one instance: what should be running. It is
// also the accepted body of POST /instances, so every field carries an explicit
// JSON name and nothing runtime-only lives here.
type Spec struct {
	ID       string   `json:"id"`
	Model    string   `json:"model"`
	TaskType TaskType `json:"task_type"`
	Port     int      `json:"port"`
	// UseGPU offloads layers to the GPU. It is a pointer so that "not specified"
	// can mean "use the GPU": a plain bool would default to false and silently run
	// CPU inference on a machine that has a perfectly good card.
	UseGPU *bool `json:"use_gpu,omitempty"`
	// GPULayers is how many layers to offload (-ngl), and is the precise form of
	// UseGPU. Offloading every layer of a model that does not fit exhausts VRAM;
	// offloading too few leaves throughput on the table, so a caller needs the
	// exact number rather than a boolean. Unset means "every layer" on a GPU host
	// and 0 when UseGPU is false.
	GPULayers *int     `json:"gpu_layers,omitempty"`
	CtxSize   int      `json:"ctx_size"`
	Threads   int      `json:"threads"`
	ExtraArgs []string `json:"extra_args,omitempty"`
}

// Validate checks every field the supervisor will later act on. It performs no
// I/O, so it can run before any process is created.
func (s Spec) Validate() error {
	if !idPattern.MatchString(s.ID) {
		return ErrInvalidID
	}
	if !s.TaskType.Valid() {
		return ErrUnknownTask
	}
	if len(s.Model) == 0 || len(s.Model) > 255 || !hasSuffixFold(s.Model, ".gguf") {
		return ErrInvalidModel
	}
	if s.Port < 1024 || s.Port > 65535 || reservedPorts[s.Port] {
		return ErrInvalidPort
	}
	if s.CtxSize != 0 && (s.CtxSize < 512 || s.CtxSize > 131072) {
		return ErrInvalidCtxSize
	}
	if s.Threads != 0 && (s.Threads < 1 || s.Threads > 256) {
		return ErrInvalidThreads
	}
	if s.GPULayers != nil && (*s.GPULayers < 0 || *s.GPULayers > 1024) {
		return ErrInvalidGPULayers
	}
	// Two knobs describe the same decision. Accepting a contradictory pair would
	// make the resolved -ngl value depend on evaluation order, so it is rejected
	// instead of silently picking one.
	if s.GPULayers != nil && s.UseGPU != nil && !*s.UseGPU && *s.GPULayers > 0 {
		return ErrConflictingGPU
	}
	return nil
}

// Normalize fills defaults for fields the caller may omit. It runs before
// Validate so that an omitted ctx_size or threads still yields a usable spec.
func (s Spec) Normalize() Spec {
	if s.CtxSize == 0 {
		s.CtxSize = DefaultCtxSize
	}
	if s.Threads == 0 {
		s.Threads = DefaultThreads
	}
	if s.Port == 0 {
		switch s.TaskType {
		case TaskRerank:
			s.Port = DefaultRerankPort
		case TaskChat:
			s.Port = DefaultChatPort
		default:
			s.Port = DefaultEmbeddingPort
		}
	}
	return s
}

// Health is the last observation the supervisor made about an instance. It is
// a measurement, not a start-time snapshot: CheckedAt lets a reader tell a fresh
// answer from a stale one, which is exactly what an untimestamped field cannot.
type Health struct {
	Reachability Reachability `json:"reachability"`
	EmbeddingDim int          `json:"embedding_dim,omitempty"`
	LatencyMS    int64        `json:"health_latency_ms,omitempty"`
	Note         string       `json:"note,omitempty"`
	CheckedAt    time.Time    `json:"checked_at"`
}

// View is the read projection returned by the HTTP API. It intentionally omits
// the model API key, the process handle and the resolved binary path.
type View struct {
	ID       string   `json:"id"`
	Model    string   `json:"model"`
	TaskType TaskType `json:"task_type"`
	Port     int      `json:"port"`
	Status   Status   `json:"status"`
	// GPULayers is the resolved -ngl value, so a reader never has to reproduce
	// the use_gpu/gpu_layers precedence to know what the process will do.
	GPULayers int        `json:"gpu_layers"`
	PID       int        `json:"pid,omitempty"`
	Endpoint  Address    `json:"endpoint"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	// Health is the most recent background observation, absent until the
	// supervisor has measured this instance at least once.
	Health    *Health `json:"health,omitempty"`
	LastError string  `json:"last_error,omitempty"`
	ExitCode  *int    `json:"exit_code,omitempty"`
}

// ViewOf projects a spec into its default read form for a stopped instance.
func ViewOf(s Spec) View {
	return View{
		ID:        s.ID,
		Model:     s.Model,
		TaskType:  s.TaskType,
		Port:      s.Port,
		Status:    StatusStopped,
		GPULayers: s.GPULayerCount(),
		Endpoint:  Address{URL: fmt.Sprintf("http://127.0.0.1:%d/v1", s.Port)},
	}
}

func hasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	tail := s[len(s)-len(suffix):]
	for i := 0; i < len(suffix); i++ {
		c, want := tail[i], suffix[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != want {
			return false
		}
	}
	return true
}

// DefaultGPULayers is the layer count used when the caller does not choose one.
// llama.cpp treats a value above the model's layer count as "offload everything",
// so 99 keeps the previous behaviour for every model this project ships.
const DefaultGPULayers = 99

// GPULayerCount resolves the -ngl value. An explicit gpu_layers always wins;
// otherwise use_gpu decides between every layer and none.
func (s Spec) GPULayerCount() int {
	if s.GPULayers != nil {
		return *s.GPULayers
	}
	if s.UseGPU != nil && !*s.UseGPU {
		return 0
	}
	return DefaultGPULayers
}

// usesGPU reports whether layers should be offloaded. An unset field means yes:
// the alternative silently downgrades a GPU host to CPU inference, which looks
// healthy and is an order of magnitude slower.
func (s Spec) usesGPU() bool { return s.GPULayerCount() > 0 }
