package modelsupervisor

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Defaults chosen to match the layout start-all.sh already creates, so the
// supervisor needs no configuration on this machine.
const (
	DefaultAddr      = "127.0.0.1:8092"
	DefaultBinaryDir = "data/bin"
	DefaultModelDir  = "data/models"
	DefaultStatePath = "data/model-supervisor-state.json"
	// DefaultWSLDistro is the distribution used for the cross-OS reachability
	// probe. It must be named explicitly: on this machine the default WSL
	// distribution is docker-desktop, which has no curl, so an unnamed probe
	// fails and every address degrades to unverified.
	DefaultWSLDistro = "Ubuntu"
	// DefaultAllowedOrigins is the console's own origin. The supervisor listens
	// on a different port, so the browser treats its API as cross-origin and
	// needs these to be spelled out; a wildcard would let any page in the
	// browser drive local model processes.
	DefaultAllowedOrigins = "http://127.0.0.1:18880,http://localhost:18880"
	// DefaultSweepInterval is how often running instances are re-probed.
	// Reachability is only useful while it is current: a model that OOMs keeps
	// its process alive but stops answering, and nothing else would notice.
	DefaultSweepInterval = 30 * time.Second
	// sweepBudget bounds one sweep. It must exceed the slowest task probe (chat
	// on CPU spends seconds in prompt processing) or a healthy instance would be
	// reported as unverified by timers it had no say in.
	sweepBudget = 60 * time.Second
)

// Config is the supervisor's whole configuration surface.
type Config struct {
	// Addr is the listen address. It must be loopback; the supervisor manages
	// local processes and has no business being reachable from the network.
	Addr string
	// Token is the shared secret for every route except /healthz.
	Token string
	// BinaryDir holds llama-server.exe and its DLLs.
	BinaryDir string
	// ModelDir holds the .gguf files.
	ModelDir string
	// StatePath is the JSON snapshot of configured instances.
	StatePath string
	// APIKey is passed to llama-server as --api-key. Empty disables it, which
	// leaves the model endpoints open to any local process.
	APIKey string
	// WSLDistro names the distribution the core runs in. The probe runs curl
	// inside it to prove the address works from both sides of the boundary.
	WSLDistro string
	// AllowedOrigins are the browser origins allowed to call the API. Empty
	// means CORS is off, which is correct for a headless deployment and wrong
	// for the console.
	AllowedOrigins []string
	LogLines       int
	StopTimeout    time.Duration
	// SweepInterval is how often running instances are re-probed. Zero disables
	// the background sweep, leaving reachability answerable only on demand.
	SweepInterval time.Duration
}

// String renders the configuration without the token or the model key.
func (c Config) String() string {
	return fmt.Sprintf("addr=%s binary=%s models=%s state=%s wsl=%s sweep=%s api_key_set=%t",
		c.Addr, c.BinaryDir, c.ModelDir, c.StatePath, c.WSLDistro, c.SweepInterval, c.APIKey != "")
}

// GoString keeps %#v from leaking the token.
func (c Config) GoString() string { return c.String() }

// LoadConfig reads the environment, applying defaults and refusing unsafe input.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("getenv required")
	}
	pick := func(key, fallback string) string {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	cfg := Config{
		Addr:      pick("MODEL_SUPERVISOR_ADDR", DefaultAddr),
		Token:     strings.TrimSpace(getenv("MODEL_SUPERVISOR_TOKEN")),
		BinaryDir: pick("MODEL_SUPERVISOR_BIN_DIR", DefaultBinaryDir),
		ModelDir:  pick("MODEL_SUPERVISOR_MODEL_DIR", DefaultModelDir),
		StatePath: pick("MODEL_SUPERVISOR_STATE", DefaultStatePath),
		APIKey:    strings.TrimSpace(getenv("MODEL_SUPERVISOR_LLAMA_API_KEY")),
		WSLDistro: pick("MODEL_SUPERVISOR_WSL_DISTRO", DefaultWSLDistro),
		LogLines:  DefaultLogLines,
	}
	for _, origin := range strings.Split(pick("MODEL_SUPERVISOR_ALLOWED_ORIGINS", DefaultAllowedOrigins), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, origin)
		}
	}
	if raw := strings.TrimSpace(getenv("MODEL_SUPERVISOR_LOG_LINES")); raw != "" {
		lines, err := strconv.Atoi(raw)
		if err != nil || lines <= 0 {
			return Config{}, fmt.Errorf("MODEL_SUPERVISOR_LOG_LINES must be a positive integer, got %q", raw)
		}
		cfg.LogLines = lines
	}
	cfg.StopTimeout = 10 * time.Second
	if raw := strings.TrimSpace(getenv("MODEL_SUPERVISOR_STOP_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("MODEL_SUPERVISOR_STOP_TIMEOUT must be a positive duration, got %q", raw)
		}
		cfg.StopTimeout = timeout
	}
	cfg.SweepInterval = DefaultSweepInterval
	if raw := strings.TrimSpace(getenv("MODEL_SUPERVISOR_SWEEP_INTERVAL")); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 0 {
			return Config{}, fmt.Errorf("MODEL_SUPERVISOR_SWEEP_INTERVAL must be zero or a positive duration, got %q", raw)
		}
		cfg.SweepInterval = interval
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces the two rules that must never be relaxed: a loopback-only
// listener and a non-empty token.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New("MODEL_SUPERVISOR_TOKEN is required: the supervisor refuses to serve unauthenticated")
	}
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("MODEL_SUPERVISOR_ADDR %q is not host:port: %w", c.Addr, err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("MODEL_SUPERVISOR_ADDR %q is not loopback: bind 127.0.0.1 only", c.Addr)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("MODEL_SUPERVISOR_ADDR %q has a non-numeric port", c.Addr)
	}
	if strings.TrimSpace(c.BinaryDir) == "" || strings.TrimSpace(c.ModelDir) == "" || strings.TrimSpace(c.StatePath) == "" {
		return errors.New("binary directory, model directory and state path are required")
	}
	for _, origin := range c.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("MODEL_SUPERVISOR_ALLOWED_ORIGINS entry %q is not an origin", origin)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("MODEL_SUPERVISOR_ALLOWED_ORIGINS entry %q must be http or https", origin)
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return fmt.Errorf("MODEL_SUPERVISOR_ALLOWED_ORIGINS entry %q must not carry a path", origin)
		}
		if strings.Contains(origin, "*") {
			return fmt.Errorf("MODEL_SUPERVISOR_ALLOWED_ORIGINS entry %q must not be a wildcard", origin)
		}
	}
	return nil
}
