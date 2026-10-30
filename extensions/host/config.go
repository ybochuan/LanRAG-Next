// Package host owns Extension Host configuration and component lifetimes.
package host

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/weknora"
)

// Config enables background side effects only in explicit worker mode. APIKey
// must never be logged; formatting and JSON serialization redact the credential.
type Config struct {
	Mode              string
	DBPath            string
	UpstreamURL       string
	APIKey            string `json:"-"`
	TenantID          uint64
	KnowledgeBaseIDs  []string
	ReconcileInterval time.Duration
	PollInterval      time.Duration
	Lease             time.Duration
	RetryDelay        time.Duration
	MaxAttempts       int
	ShutdownTimeout   time.Duration
}

// String deliberately excludes configuration values, including KB metadata.
func (Config) String() string { return "AgentRAG host configuration (redacted)" }

// GoString also protects %#v formatting.
func (c Config) GoString() string { return c.String() }

// LoadConfig reads process environment through the supplied function; it does
// not read .env files, create a database or contact the upstream API.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{Mode: "probe", ReconcileInterval: time.Minute, PollInterval: time.Second, Lease: time.Minute, RetryDelay: 5 * time.Second, MaxAttempts: 5, ShutdownTimeout: 10 * time.Second}
	if getenv == nil {
		return c, errors.New("environment reader required")
	}
	if mode := getenv("AGENTRAG_MODE"); mode != "" {
		c.Mode = strings.TrimSpace(mode)
	}
	workerKeys := []string{"AGENTRAG_EXT_DB", "AGENTRAG_WEKNORA_URL", "AGENTRAG_WEKNORA_API_KEY", "AGENTRAG_TENANT_ID", "AGENTRAG_KB_IDS", "AGENTRAG_RECONCILE_INTERVAL", "AGENTRAG_OUTBOX_POLL_INTERVAL", "AGENTRAG_OUTBOX_LEASE", "AGENTRAG_OUTBOX_RETRY_DELAY", "AGENTRAG_OUTBOX_MAX_ATTEMPTS"}
	if c.Mode == "probe" {
		for _, key := range workerKeys {
			if getenv(key) != "" {
				return c, errors.New("worker options require AGENTRAG_MODE=worker")
			}
		}
	}
	c.DBPath, c.UpstreamURL, c.APIKey = getenv("AGENTRAG_EXT_DB"), getenv("AGENTRAG_WEKNORA_URL"), getenv("AGENTRAG_WEKNORA_API_KEY")
	if raw := getenv("AGENTRAG_TENANT_ID"); raw != "" {
		tenant, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return c, errors.New("invalid AGENTRAG_TENANT_ID")
		}
		c.TenantID = tenant
	}
	if raw := getenv("AGENTRAG_KB_IDS"); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			c.KnowledgeBaseIDs = append(c.KnowledgeBaseIDs, strings.TrimSpace(id))
		}
	}
	for _, setting := range []struct {
		name  string
		value *time.Duration
	}{
		{"AGENTRAG_RECONCILE_INTERVAL", &c.ReconcileInterval}, {"AGENTRAG_OUTBOX_POLL_INTERVAL", &c.PollInterval},
		{"AGENTRAG_OUTBOX_LEASE", &c.Lease}, {"AGENTRAG_OUTBOX_RETRY_DELAY", &c.RetryDelay}, {"AGENTRAG_SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
	} {
		if raw := getenv(setting.name); raw != "" {
			duration, err := time.ParseDuration(raw)
			if err != nil {
				return c, fmt.Errorf("invalid %s", setting.name)
			}
			*setting.value = duration
		}
	}
	if raw := getenv("AGENTRAG_OUTBOX_MAX_ATTEMPTS"); raw != "" {
		attempts, err := strconv.Atoi(raw)
		if err != nil {
			return c, errors.New("invalid AGENTRAG_OUTBOX_MAX_ATTEMPTS")
		}
		c.MaxAttempts = attempts
	}
	return c, c.Validate()
}

// Validate is also required for programmatically constructed configurations.
func (c Config) Validate() error {
	if c.Mode != "probe" && c.Mode != "worker" {
		return errors.New("AGENTRAG_MODE must be probe or worker")
	}
	for _, duration := range []time.Duration{c.ReconcileInterval, c.PollInterval, c.Lease, c.RetryDelay, c.ShutdownTimeout} {
		if duration < time.Millisecond || duration > 24*time.Hour {
			return errors.New("host durations must be between 1ms and 24h")
		}
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 100 {
		return errors.New("outbox attempts must be 1..100")
	}
	if c.Mode == "probe" {
		if c.DBPath != "" || c.UpstreamURL != "" || c.APIKey != "" || c.TenantID != 0 || len(c.KnowledgeBaseIDs) != 0 {
			return errors.New("probe configuration contains worker options")
		}
		return nil
	}
	if strings.TrimSpace(c.DBPath) == "" || c.DBPath == ":memory:" || strings.HasPrefix(c.DBPath, "file:") {
		return errors.New("independent extension database filesystem path required")
	}
	if _, err := weknora.NewClient(c.UpstreamURL, c.APIKey, c.TenantID); err != nil {
		return errors.New("invalid worker API origin, key or tenant")
	}
	if len(c.KnowledgeBaseIDs) < 1 || len(c.KnowledgeBaseIDs) > 1000 {
		return errors.New("worker requires 1..1000 knowledge bases")
	}
	seen := make(map[string]bool)
	for _, id := range c.KnowledgeBaseIDs {
		if !weknora.ValidID(id) || seen[id] {
			return errors.New("invalid or duplicate worker knowledge base")
		}
		seen[id] = true
	}
	return nil
}
