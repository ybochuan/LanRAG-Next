package host

import (
	"path/filepath"
	"testing"
)

func TestWorkerDefaultDatabaseIsIndependentAbsolutePath(t *testing.T) {
	values := workerEnv()
	delete(values, "AGENTRAG_EXT_DB")
	cfg, err := LoadConfig(envReader(values))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.DBPath) || filepath.Base(cfg.DBPath) != "agentrag_ext.db" {
		t.Fatalf("unsafe default: %s", cfg.DBPath)
	}
}
