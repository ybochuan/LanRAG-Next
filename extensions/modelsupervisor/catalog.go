package modelsupervisor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ModelFile is one launchable model file found on disk.
type ModelFile struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	Modified  time.Time `json:"modified"`
}

// Catalog describes what the supervisor could launch right now: the .gguf files
// present in the model directory and the runtime they would run under. It is a
// read-only view of the filesystem; it never creates, moves or deletes files.
type Catalog struct {
	ModelDir  string `json:"model_dir"`
	BinaryDir string `json:"binary_dir"`
	// Binary is the resolved path the manager would exec.
	Binary string `json:"binary"`
	// BinaryReady reports whether that path currently exists.
	BinaryReady bool        `json:"binary_ready"`
	Models      []ModelFile `json:"models"`
	// Note explains an empty result that is not an error, such as a model
	// directory that has not been created yet.
	Note string `json:"note,omitempty"`
}

// ScanCatalog lists the .gguf files in modelDir, sorted by name. A missing model
// directory is reported through Catalog.Note rather than an error: a fresh
// install legitimately has no models yet, and the console should say so instead
// of rendering a failure banner.
func ScanCatalog(modelDir, binaryDir string) (Catalog, error) {
	if strings.TrimSpace(modelDir) == "" || strings.TrimSpace(binaryDir) == "" {
		return Catalog{}, errors.New("model and binary directories are required")
	}
	modelsAbs, err := filepath.Abs(modelDir)
	if err != nil {
		return Catalog{}, fmt.Errorf("resolve model directory: %w", err)
	}
	binaryAbs, err := filepath.Abs(binaryDir)
	if err != nil {
		return Catalog{}, fmt.Errorf("resolve binary directory: %w", err)
	}

	catalog := Catalog{
		ModelDir:  modelsAbs,
		BinaryDir: binaryAbs,
		Binary:    filepath.Join(binaryAbs, BinaryName),
		Models:    []ModelFile{},
	}
	if info, err := os.Stat(catalog.Binary); err == nil && info.Mode().IsRegular() {
		catalog.BinaryReady = true
	}

	entries, err := os.ReadDir(modelsAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			catalog.Note = fmt.Sprintf("模型目录 %s 不存在；把 .gguf 放进去后重新扫描", modelsAbs)
			return catalog, nil
		}
		return Catalog{}, fmt.Errorf("read model directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gguf") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			// A file that vanished between ReadDir and Info, or a device node,
			// is not something this manager can launch.
			continue
		}
		catalog.Models = append(catalog.Models, ModelFile{
			Name:      entry.Name(),
			SizeBytes: info.Size(),
			Modified:  info.ModTime().UTC(),
		})
	}
	sort.Slice(catalog.Models, func(i, j int) bool {
		return catalog.Models[i].Name < catalog.Models[j].Name
	})
	return catalog, nil
}
