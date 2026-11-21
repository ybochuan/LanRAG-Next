package agentrag

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Host manages the installed Extension Host for one native desktop profile.
type Host struct {
	dir, binary string
	current     state
	cancel      context.CancelFunc
	done        chan struct{}
}

// New prepares persistent credentials before the native router starts.
func New(dir, binary string) (*Host, error) {
	s, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	settings := map[string]string{
		"AGENTRAG_MANAGEMENT_KEY":         s.Management,
		"AGENTRAG_BEFORE_GENERATE_KEY":    s.BeforeGenerate,
		"AGENTRAG_CORRECTION_SOURCES_KEY": s.Corrections,
		"AGENTRAG_PARSER_PROXY_ADDR":      "",
	}
	if os.Getenv("SYSTEM_AES_KEY") == "" {
		settings["SYSTEM_AES_KEY"] = s.AESKey
	}
	if os.Getenv("AGENTRAG_PARSER_UPSTREAM_URL") != "" {
		settings["AGENTRAG_PARSER_PROXY_ADDR"] = "http://127.0.0.1:8090"
		settings["AGENTRAG_PARSER_PROXY_KEY"] = s.Parser
	}
	for k, v := range settings {
		if err := os.Setenv(k, v); err != nil {
			return nil, err
		}
	}
	return &Host{dir: dir, binary: binary, current: s}, nil
}

// Start waits for the signed-in local desktop account to have a knowledge base.
func (h *Host) Start(coreURL, setupToken string, report func(string)) {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan struct{})
	go func() {
		defer close(h.done)
		h.run(ctx, coreURL, setupToken, report)
	}()
}

// Stop closes the child's stdin and joins its graceful shutdown.
func (h *Host) Stop() {
	if h.cancel != nil {
		h.cancel()
		<-h.done
	}
}

func (h *Host) run(ctx context.Context, url, setupToken string, report func(string)) {
	client := newCoreClient(url, setupToken)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	report("agentrag_waiting_for_knowledge_base")
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		tenant, ids, err := client.scope(ctx)
		if err != nil || len(ids) == 0 {
			continue
		}
		previous := h.current
		changed, err := client.bind(ctx, &h.current, tenant, ids)
		if err != nil {
			report("agentrag_scope_binding_failed")
			return
		}
		if changed {
			if err := saveState(h.dir, h.current); err != nil {
				_ = client.revoke(ctx, h.current)
				report("agentrag_scope_persist_failed")
				return
			}
			if previous.Tenant == h.current.Tenant && client.revoke(ctx, previous) != nil {
				report("agentrag_previous_key_revoke_failed")
			}
		}
		tenantString, kbString := strconv.FormatUint(tenant, 10), strings.Join(ids, ",")
		_ = os.Setenv("AGENTRAG_TENANT_ID", tenantString)
		_ = os.Setenv("AGENTRAG_KB_IDS", kbString)
		register := func(childCtx context.Context) {
			if err := client.provisionHostMCP(childCtx, h.current.Management); err != nil {
				if errors.Is(err, errHostMCPDisabled) {
					report("agentrag_mcp_disabled")
				} else if childCtx.Err() == nil {
					report("agentrag_mcp_registration_failed: " + err.Error())
				}
				return
			}
			report("agentrag_mcp_ready")
		}
		if err := h.child(ctx, url, tenantString, kbString, ids, register); err != nil && ctx.Err() == nil {
			report("agentrag_host_stopped_check_log")
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (h *Host) child(ctx context.Context, url, tenant, kbs string, ids []string, started func(context.Context)) error {
	log, err := os.OpenFile(filepath.Join(h.dir, "extension-host.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(h.binary, "-parent-stdin")
	cmd.Dir = h.dir
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(),
		"AGENTRAG_MODE=worker", "AGENTRAG_EXT_DB="+filepath.Join(h.dir, "agentrag_ext.db"),
		"AGENTRAG_WEKNORA_URL="+url, "AGENTRAG_WEKNORA_API_KEY="+h.current.APIKey,
		"AGENTRAG_TENANT_ID="+tenant, "AGENTRAG_KB_IDS="+kbs,
		"AGENTRAG_CORRECTION_DIR="+filepath.Join(h.dir, "corrections"))
	if os.Getenv("AGENTRAG_PARSER_UPSTREAM_URL") != "" {
		type scope struct {
			Tenant uint64 `json:"tenant_id"`
			KB     string `json:"knowledge_base_id"`
		}
		scopes := make([]scope, 0, len(ids))
		for _, id := range ids {
			scopes = append(scopes, scope{h.current.Tenant, id})
		}
		encoded, _ := json.Marshal(scopes)
		cmd.Env = append(cmd.Env, "AGENTRAG_PARSER_SCOPES="+string(encoded),
			"AGENTRAG_PARSER_ARTIFACT_ROOT="+filepath.Join(h.dir, "parser_artifacts"))
	}
	return runChild(ctx, cmd, started)
}

func runChild(ctx context.Context, cmd *exec.Cmd, started func(context.Context)) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		done <- cmd.Wait()
		cancel()
	}()
	started(childCtx)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	_ = stdin.Close()
	select {
	case err := <-done:
		return err
	case <-time.After(12 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return errors.New("extension Host shutdown deadline exceeded")
	}
}
