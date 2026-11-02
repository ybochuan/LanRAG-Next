package modelsupervisor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validSpec() Spec {
	return Spec{ID: "embedding", Model: "Qwen3-Embedding-4B-Q6_K.gguf", TaskType: TaskEmbedding, Port: 8081, CtxSize: 4096, Threads: 8}
}

func TestSpecValidateAcceptsBaseline(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("baseline spec rejected: %v", err)
	}
	if err := (Spec{ID: "rerank", Model: "a.GGUF", TaskType: TaskRerank, Port: 8082}).Validate(); err != nil {
		t.Fatalf("minimal rerank spec rejected: %v", err)
	}
}

func TestSpecValidateRejectsUnsafeInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Spec)
		want   error
	}{
		{"empty id", func(s *Spec) { s.ID = "" }, ErrInvalidID},
		{"upper id", func(s *Spec) { s.ID = "Embedding" }, ErrInvalidID},
		{"path id", func(s *Spec) { s.ID = "../embedding" }, ErrInvalidID},
		{"unknown task", func(s *Spec) { s.TaskType = "llm" }, ErrUnknownTask},
		{"empty model", func(s *Spec) { s.Model = "" }, ErrInvalidModel},
		{"non gguf", func(s *Spec) { s.Model = "model.bin" }, ErrInvalidModel},
		{"model path", func(s *Spec) { s.Model = `..\..\windows\system32\cmd.exe` }, ErrInvalidModel},
		{"privileged port", func(s *Spec) { s.Port = 80 }, ErrInvalidPort},
		{"zero port", func(s *Spec) { s.Port = 0 }, ErrInvalidPort},
		{"reserved port", func(s *Spec) { s.Port = 8090 }, ErrInvalidPort},
		{"tiny ctx", func(s *Spec) { s.CtxSize = 128 }, ErrInvalidCtxSize},
		{"huge ctx", func(s *Spec) { s.CtxSize = 999999 }, ErrInvalidCtxSize},
		{"negative threads", func(s *Spec) { s.Threads = -1 }, ErrInvalidThreads},
	} {
		spec := validSpec()
		tc.mutate(&spec)
		if err := spec.Validate(); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestSpecNormalizeFillsDefaultsPerTaskType(t *testing.T) {
	embedding := Spec{ID: "e", Model: "m.gguf", TaskType: TaskEmbedding}.Normalize()
	if embedding.Port != DefaultEmbeddingPort || embedding.CtxSize != DefaultCtxSize || embedding.Threads != DefaultThreads {
		t.Fatalf("embedding defaults=%+v", embedding)
	}
	rerank := Spec{ID: "r", Model: "m.gguf", TaskType: TaskRerank}.Normalize()
	if rerank.Port != DefaultRerankPort {
		t.Fatalf("rerank default port=%d", rerank.Port)
	}
	chat := Spec{ID: "c", Model: "m.gguf", TaskType: TaskChat}.Normalize()
	if chat.Port != DefaultChatPort {
		t.Fatalf("chat default port=%d", chat.Port)
	}
	explicit := Spec{ID: "e", Model: "m.gguf", TaskType: TaskEmbedding, Port: 9100, CtxSize: 2048, Threads: 2}.Normalize()
	if explicit.Port != 9100 || explicit.CtxSize != 2048 || explicit.Threads != 2 {
		t.Fatalf("explicit values overwritten: %+v", explicit)
	}
}

func TestViewOfCarriesNoRuntimeOnlyFields(t *testing.T) {
	view := ViewOf(validSpec())
	if view.Status != StatusStopped || view.Endpoint.URL != "http://127.0.0.1:8081/v1" {
		t.Fatalf("view=%+v", view)
	}
	if view.PID != 0 || view.StartedAt != nil || view.Health != nil {
		t.Fatalf("stopped view exposed runtime state: %+v", view)
	}
}

// A stopped view must not claim anything about reachability: an address nobody
// has measured is exactly the field this projection used to carry and should not.
func TestViewOfClaimsNoReachability(t *testing.T) {
	raw, err := json.Marshal(ViewOf(validSpec()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"reachability", "note", "health"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("stopped view still reports %q: %s", forbidden, raw)
		}
	}
}

func TestViewJSONHasExplicitNames(t *testing.T) {
	view := View{ID: "e", Model: "m.gguf", TaskType: TaskEmbedding, Port: 8081, Status: StatusRunning, StartedAt: ptrTime()}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	encoded := string(raw)
	for _, want := range []string{`"id"`, `"model"`, `"task_type"`, `"port"`, `"status"`, `"endpoint"`, `"started_at"`} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("view json missing %s: %s", want, encoded)
		}
	}
	for _, forbidden := range []string{"api_key", "cmd", "mu", "binary"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("view json leaked %s: %s", forbidden, encoded)
		}
	}
}

func ptrTime() *time.Time {
	now := time.Unix(0, 0).UTC()
	return &now
}

func TestSpecUsesGPUByDefault(t *testing.T) {
	spec := Spec{ID: "embedding", Model: "m.gguf", TaskType: TaskEmbedding, Port: 8081}
	if !spec.usesGPU() {
		t.Fatal("an unset use_gpu must default to GPU offload")
	}
	no := false
	spec.UseGPU = &no
	if spec.usesGPU() {
		t.Fatal("use_gpu=false must mean CPU only")
	}
	yes := true
	spec.UseGPU = &yes
	if !spec.usesGPU() {
		t.Fatal("use_gpu=true must mean GPU offload")
	}
}

func TestSpecGPULayerPrecedence(t *testing.T) {
	base := Spec{ID: "embedding", Model: "m.gguf", TaskType: TaskEmbedding, Port: 8081}
	if got := base.GPULayerCount(); got != DefaultGPULayers {
		t.Fatalf("unset gpu_layers => %d, want %d", got, DefaultGPULayers)
	}
	no := false
	base.UseGPU = &no
	if got := base.GPULayerCount(); got != 0 {
		t.Fatalf("use_gpu=false => %d, want 0", got)
	}
	// An explicit count is the precise knob and outranks the coarse boolean.
	partial := 20
	base.GPULayers = &partial
	if got := base.GPULayerCount(); got != 20 {
		t.Fatalf("explicit gpu_layers => %d, want 20", got)
	}
	if !base.usesGPU() {
		t.Fatal("a positive gpu_layers must count as GPU offload")
	}
	zero := 0
	base.UseGPU = nil
	base.GPULayers = &zero
	if base.usesGPU() {
		t.Fatal("gpu_layers=0 must mean CPU only")
	}
}

func TestValidateRejectsBadGPULayers(t *testing.T) {
	spec := validSpec()
	tooMany := 1025
	spec.GPULayers = &tooMany
	if err := spec.Validate(); !errors.Is(err, ErrInvalidGPULayers) {
		t.Fatalf("gpu_layers=1025 => %v, want %v", err, ErrInvalidGPULayers)
	}
	negative := -1
	spec.GPULayers = &negative
	if err := spec.Validate(); !errors.Is(err, ErrInvalidGPULayers) {
		t.Fatalf("gpu_layers=-1 => %v, want %v", err, ErrInvalidGPULayers)
	}
	// use_gpu=false with offloaded layers describes two different machines.
	no := false
	partial := 20
	spec.UseGPU = &no
	spec.GPULayers = &partial
	if err := spec.Validate(); !errors.Is(err, ErrConflictingGPU) {
		t.Fatalf("use_gpu=false + gpu_layers=20 => %v, want %v", err, ErrConflictingGPU)
	}
	// The agreeing pair is accepted.
	spec.GPULayers = &zeroLayers
	if err := spec.Validate(); err != nil {
		t.Fatalf("use_gpu=false + gpu_layers=0 => %v, want nil", err)
	}
}

var zeroLayers = 0

// The probe timeout must admit the one-off cost llama.cpp pays on the first
// request through its graph, otherwise a healthy server reads as unreachable
// (measured here: 12.3s, then 5.0s, then 0.10s).
func TestTaskTimeoutAllowsColdEmbeddingInference(t *testing.T) {
	if got := TaskEmbedding.taskTimeout(); got < 15*time.Second {
		t.Fatalf("embedding probe budget = %s, too short for cold inference", got)
	}
	if got := TaskChat.taskTimeout(); got < 30*time.Second {
		t.Fatalf("chat probe budget = %s, too short for CPU prompt processing", got)
	}
	if got, want := TaskRerank.taskTimeout(), 3*time.Second; got != want {
		t.Fatalf("rerank probe budget = %s, want %s", got, want)
	}
	if sweepBudget <= TaskChat.taskTimeout() {
		t.Fatalf("sweep budget %s must exceed the longest probe timeout", sweepBudget)
	}
}
