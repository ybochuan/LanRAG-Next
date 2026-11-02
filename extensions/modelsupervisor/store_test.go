package modelsupervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripSortsInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	rerank := Spec{ID: "rerank", Model: "r.gguf", TaskType: TaskRerank, Port: 8082, CtxSize: 4096, Threads: 8}
	embedding := validSpec()
	if err := store.Save(State{Instances: []Spec{rerank, embedding}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if state.Schema != stateSchema || len(state.Instances) != 2 {
		t.Fatalf("state=%+v", state)
	}
	if state.Instances[0].ID != "embedding" || state.Instances[1].ID != "rerank" {
		t.Fatalf("instances not sorted: %+v", state.Instances)
	}
}

func TestStoreLoadMissingFileIsEmptyState(t *testing.T) {
	state, err := NewStore(filepath.Join(t.TempDir(), "absent.json")).Load()
	if err != nil || state.Schema != stateSchema || len(state.Instances) != 0 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestStoreLeavesCorruptFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	corrupt := []byte("{not json")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("corrupt state accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(corrupt) {
		t.Fatalf("load rewrote the corrupt file: %q err=%v", after, err)
	}
}

func TestStoreRejectsFutureSchemaAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	future := filepath.Join(dir, "future.json")
	if err := os.WriteFile(future, []byte(`{"schema":2,"instances":[]}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewStore(future).Load(); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("future schema accepted: %v", err)
	}
	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"schema":1,"instances":[],"extra":true}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := NewStore(unknown).Load(); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestStoreRejectsDuplicateOrInvalidPersistedInstances(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"duplicate id", `{"schema":1,"instances":[
			{"id":"e","model":"m.gguf","task_type":"embedding","port":8081,"use_gpu":true,"ctx_size":4096,"threads":8},
			{"id":"e","model":"m.gguf","task_type":"embedding","port":8082,"use_gpu":true,"ctx_size":4096,"threads":8}]}`},
		{"duplicate port", `{"schema":1,"instances":[
			{"id":"e","model":"m.gguf","task_type":"embedding","port":8081,"use_gpu":true,"ctx_size":4096,"threads":8},
			{"id":"r","model":"m.gguf","task_type":"rerank","port":8081,"use_gpu":true,"ctx_size":4096,"threads":8}]}`},
		{"reserved port", `{"schema":1,"instances":[
			{"id":"e","model":"m.gguf","task_type":"embedding","port":8090,"use_gpu":true,"ctx_size":4096,"threads":8}]}`},
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatalf("%s seed: %v", tc.name, err)
		}
		if _, err := NewStore(path).Load(); err == nil {
			t.Fatalf("%s: accepted", tc.name)
		}
	}
}

func TestStoreSaveReplacesAtomicallyWithoutLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStore(path)
	if err := store.Save(State{Instances: []Spec{validSpec()}}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	second := validSpec()
	second.Threads = 4
	if err := store.Save(State{Instances: []Spec{second}}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	state, err := store.Load()
	if err != nil || len(state.Instances) != 1 || state.Instances[0].Threads != 4 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestStoreRequiresPath(t *testing.T) {
	if _, err := NewStore("").Load(); err == nil {
		t.Fatal("empty path accepted on load")
	}
	if err := NewStore("").Save(State{}); err == nil {
		t.Fatal("empty path accepted on save")
	}
}
