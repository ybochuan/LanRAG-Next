package modelsupervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanCatalogListsOnlyGGUF(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "models")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	write := func(name string, size int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(modelDir, name), make([]byte, size), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("zeta-embedding.GGUF", 11)
	write("alpha-reranker.gguf", 7)
	write("notes.txt", 3)
	write("half-download.gguf.part", 5)
	if err := os.MkdirAll(filepath.Join(modelDir, "nested.gguf"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	catalog, err := ScanCatalog(modelDir, binDir)
	if err != nil {
		t.Fatalf("ScanCatalog: %v", err)
	}

	if len(catalog.Models) != 2 {
		t.Fatalf("got %d models, want 2: %+v", len(catalog.Models), catalog.Models)
	}
	if catalog.Models[0].Name != "alpha-reranker.gguf" || catalog.Models[1].Name != "zeta-embedding.GGUF" {
		t.Fatalf("models not sorted by name: %+v", catalog.Models)
	}
	if catalog.Models[0].SizeBytes != 7 {
		t.Fatalf("size = %d, want 7", catalog.Models[0].SizeBytes)
	}
	if catalog.BinaryReady {
		t.Fatal("binary_ready true without llama-server.exe on disk")
	}
	if catalog.Binary != filepath.Join(catalog.BinaryDir, BinaryName) {
		t.Fatalf("binary path = %q", catalog.Binary)
	}
	if catalog.Note != "" {
		t.Fatalf("unexpected note: %q", catalog.Note)
	}
}

func TestScanCatalogReportsMissingDirectoryAsNote(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "absent")

	catalog, err := ScanCatalog(modelDir, filepath.Join(root, "bin"))
	if err != nil {
		t.Fatalf("missing model directory must not be an error: %v", err)
	}
	if len(catalog.Models) != 0 {
		t.Fatalf("got %d models, want 0", len(catalog.Models))
	}
	if catalog.Note == "" {
		t.Fatal("missing directory must carry a note so the console can explain it")
	}
	if catalog.Models == nil {
		t.Fatal("models must be an empty slice, not nil: JSON would render null")
	}
}

func TestScanCatalogDetectsBinary(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	modelDir := filepath.Join(root, "models")
	for _, dir := range []string{binDir, modelDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, BinaryName), []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	catalog, err := ScanCatalog(modelDir, binDir)
	if err != nil {
		t.Fatalf("ScanCatalog: %v", err)
	}
	if !catalog.BinaryReady {
		t.Fatal("binary_ready false with llama-server.exe present")
	}
}

func TestScanCatalogRejectsEmptyDirectories(t *testing.T) {
	if _, err := ScanCatalog("", "bin"); err == nil {
		t.Fatal("empty model directory must be rejected")
	}
	if _, err := ScanCatalog("models", "  "); err == nil {
		t.Fatal("blank binary directory must be rejected")
	}
}
