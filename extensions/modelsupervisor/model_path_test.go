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
