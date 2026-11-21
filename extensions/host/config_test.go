package host

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func workerEnv() map[string]string {
	return map[string]string{"AGENTRAG_MODE": "worker", "AGENTRAG_EXT_DB": "agentrag_ext.db", "AGENTRAG_WEKNORA_URL": "http://127.0.0.1:8080", "AGENTRAG_WEKNORA_API_KEY": "test-secret", "AGENTRAG_TENANT_ID": "7", "AGENTRAG_KB_IDS": "kb-a,kb-b"}
}

func envReader(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestConfigDefaultsAndCompleteWorker(t *testing.T) {
	cfg, err := LoadConfig(envReader(nil))
	if err != nil || cfg.Mode != "probe" || cfg.DBPath != "" || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("probe=%v %v", cfg, err)
	}
	cfg, err = LoadConfig(envReader(workerEnv()))
	if err != nil || cfg.Mode != "worker" || cfg.TenantID != 7 || len(cfg.KnowledgeBaseIDs) != 2 || cfg.ReconcileInterval != time.Minute || cfg.PollInterval != time.Second || cfg.Lease != time.Minute || cfg.RetryDelay != 5*time.Second || cfg.MaxAttempts != 5 {
		t.Fatalf("worker=%v %v", cfg, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, cfg), "test-secret") {
			t.Fatal("configuration formatting leaked credential")
		}
	}
}

func TestConfigCorrectionRollout(t *testing.T) {
	values := workerEnv()
	values["AGENTRAG_CORRECTION_DIR"] = "test-correction-root"
	values["AGENTRAG_CORRECTION_KB_IDS"] = " kb-b "
	cfg, err := LoadConfig(envReader(values))
	if err != nil || cfg.CorrectionDir != "test-correction-root" || len(cfg.CorrectionKBIDs) != 1 || cfg.CorrectionKBIDs[0] != "kb-b" {
		t.Fatalf("wrong rollout scope: %+v %v", cfg.CorrectionKBIDs, err)
	}
	values["AGENTRAG_CORRECTION_KB_IDS"] = "[]"
	cfg, err = LoadConfig(envReader(values))
	if err != nil || cfg.CorrectionKBIDs == nil || len(cfg.CorrectionKBIDs) != 0 {
		t.Fatalf("explicit disable must differ from unset: %+v %v", cfg.CorrectionKBIDs, err)
	}
}

func TestConfigValidatesHookCredentialsAsSeparateAudiences(t *testing.T) {
	values := workerEnv()
	values["AGENTRAG_MANAGEMENT_KEY"] = strings.Repeat("m", 32)
	values["AGENTRAG_CORRECTION_SOURCES_KEY"] = strings.Repeat("c", 32)
	if _, err := LoadConfig(envReader(values)); err == nil {
		t.Fatal("partial hook credentials accepted")
	}
	for _, key := range []string{values["AGENTRAG_MANAGEMENT_KEY"], values["AGENTRAG_CORRECTION_SOURCES_KEY"], "short"} {
		values["AGENTRAG_BEFORE_GENERATE_KEY"] = key
		if _, err := LoadConfig(envReader(values)); err == nil {
			t.Fatal("reused or short hook credential accepted")
		}
	}
	values["AGENTRAG_BEFORE_GENERATE_KEY"] = strings.Repeat("g", 32)
	if _, err := LoadConfig(envReader(values)); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRejectsInvalidInputsWithoutEchoingValues(t *testing.T) {
	for _, change := range []func(map[string]string){
		func(e map[string]string) { delete(e, "AGENTRAG_WEKNORA_API_KEY") },
		func(e map[string]string) { e["AGENTRAG_WEKNORA_URL"] = "http://user:test-secret@localhost" },
		func(e map[string]string) { e["AGENTRAG_TENANT_ID"] = "test-secret" },
		func(e map[string]string) { e["AGENTRAG_KB_IDS"] = "kb-a,kb-a" },
		func(e map[string]string) { e["AGENTRAG_KB_IDS"] = "kb-a," },
		func(e map[string]string) { e["AGENTRAG_CORRECTION_KB_IDS"] = "test-secret" },
		func(e map[string]string) { e["AGENTRAG_CORRECTION_KB_IDS"] = "kb-b,kb-b" },
		func(e map[string]string) { e["AGENTRAG_RECONCILE_INTERVAL"] = "0s" },
		func(e map[string]string) { e["AGENTRAG_OUTBOX_LEASE"] = "1ns" },
		func(e map[string]string) { e["AGENTRAG_SHUTDOWN_TIMEOUT"] = "25h" },
		func(e map[string]string) { e["AGENTRAG_OUTBOX_MAX_ATTEMPTS"] = "0" },
		func(e map[string]string) { e["AGENTRAG_MODE"] = "probe" },
		func(e map[string]string) { e["AGENTRAG_MODE"] = "test-secret" },
	} {
		values := workerEnv()
		change(values)
		if _, err := LoadConfig(envReader(values)); err == nil || strings.Contains(err.Error(), "test-secret") {
			t.Fatalf("unsafe validation: %v", err)
		}
	}
}
