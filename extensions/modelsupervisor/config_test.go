package modelsupervisor

import (
	"strings"
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	cfg, err := LoadConfig(envFrom(map[string]string{"MODEL_SUPERVISOR_TOKEN": "secret"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.BinaryDir != DefaultBinaryDir || cfg.ModelDir != DefaultModelDir || cfg.StatePath != DefaultStatePath {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.LogLines != DefaultLogLines {
		t.Fatalf("log lines = %d", cfg.LogLines)
	}
	if cfg.StopTimeout != 10*time.Second {
		t.Fatalf("stop timeout = %s", cfg.StopTimeout)
	}
}

func TestLoadConfigRefusesUnsafeInput(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no token", map[string]string{}, "MODEL_SUPERVISOR_TOKEN"},
		{"blank token", map[string]string{"MODEL_SUPERVISOR_TOKEN": "   "}, "MODEL_SUPERVISOR_TOKEN"},
		{"wildcard bind", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_ADDR": "0.0.0.0:8092"}, "not loopback"},
		{"lan bind", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_ADDR": "192.168.1.10:8092"}, "not loopback"},
		{"bad addr", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_ADDR": "8092"}, "not host:port"},
		{"non numeric port", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_ADDR": "127.0.0.1:http"}, "non-numeric port"},
		{"bad log lines", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_LOG_LINES": "0"}, "MODEL_SUPERVISOR_LOG_LINES"},
		{"bad timeout", map[string]string{"MODEL_SUPERVISOR_TOKEN": "s", "MODEL_SUPERVISOR_STOP_TIMEOUT": "-1s"}, "MODEL_SUPERVISOR_STOP_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(envFrom(tc.env))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestConfigNeverPrintsSecrets(t *testing.T) {
	cfg := Config{
		Addr: "127.0.0.1:8092", Token: "super-secret-token", APIKey: "llama-secret",
		BinaryDir: "data/bin", ModelDir: "data/models", StatePath: "data/state.json",
	}
	rendered := cfg.String() + " " + cfg.GoString()
	if strings.Contains(rendered, "super-secret-token") || strings.Contains(rendered, "llama-secret") {
		t.Fatalf("config rendering leaked a secret: %s", rendered)
	}
	if !strings.Contains(rendered, "api_key_set=true") {
		t.Fatalf("rendered = %q, want the presence of the key to be visible", rendered)
	}
}

func TestConfigAcceptsLocalhostAlias(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8092", "localhost:8092", "[::1]:8092"} {
		cfg := Config{
			Addr: addr, Token: "s", BinaryDir: "b", ModelDir: "m", StatePath: "p",
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
}
