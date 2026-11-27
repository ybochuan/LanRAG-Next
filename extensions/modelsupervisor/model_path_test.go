package modelsupervisor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedModelFileUsesOriginalPathOnlyInDesktop(t *testing.T) {
	const name = "测试 模型.gguf"
	selected := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(selected, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "local", Model: name, ModelPath: selected, TaskType: TaskEmbedding, Port: 8081}
	manager := &Manager{opts: Options{ModelDir: t.TempDir()}}
	if _, _, err := manager.ModelPath(spec); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("external model path should require Desktop: %v", err)
	}
	manager.opts.AllowSelectedFiles = true
	path, size, err := manager.ModelPath(spec)
	if err != nil || path != selected || size != 4 {
		t.Fatalf("selected path=%q size=%d err=%v", path, size, err)
	}
	if ViewOf(spec).ModelPath != selected {
		t.Fatal("instance projection must retain the selected file for restart")
	}
}

func TestProjectorPathFollowsTheSelectedFileRules(t *testing.T) {
	const name = "mmproj-Qwen3-ASR.gguf"
	selected := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(selected, []byte("GGUF"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "asr", Model: "asr.gguf", TaskType: TaskASR, Port: 8084, MMProj: name, MMProjPath: selected}
	manager := &Manager{opts: Options{ModelDir: t.TempDir()}}
	if _, _, err := manager.ProjectorPath(spec); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("external projector path should require Desktop: %v", err)
	}
	manager.opts.AllowSelectedFiles = true
	path, size, err := manager.ProjectorPath(spec)
	if err != nil || path != selected || size != 4 {
		t.Fatalf("selected projector path=%q size=%d err=%v", path, size, err)
	}
	missing := spec
	missing.MMProjPath = filepath.Join(t.TempDir(), name)
	if _, _, err := manager.ProjectorPath(missing); err == nil {
		t.Fatal("missing projector must be rejected")
	}
}
