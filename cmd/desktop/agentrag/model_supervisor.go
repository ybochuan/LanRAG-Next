package agentrag

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"
)

const supervisorVersionHeader = "X-AgentRAG-Model-Supervisor-Version"

// ModelSupervisor connects the installed desktop window to its local model process manager.
type ModelSupervisor struct {
	profile, binary string
	cancel          context.CancelFunc
	done            chan struct{}
	proxy           atomic.Pointer[httputil.ReverseProxy]
}

func NewModelSupervisor(profile, binary string) *ModelSupervisor {
	return &ModelSupervisor{profile: profile, binary: binary}
}

func (s *ModelSupervisor) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		defer s.proxy.Store(nil)
		s.run(ctx, cancel)
	}()
}

func (s *ModelSupervisor) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func (s *ModelSupervisor) run(ctx context.Context, cancel context.CancelFunc) {
	log, err := os.OpenFile(filepath.Join(s.profile, "model-supervisor.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer log.Close()

	key := rand.Text()
	cmd := exec.Command(s.binary, "-parent-stdin")
	cmd.Dir, cmd.Stderr = s.profile, log
	cmd.Env = append(os.Environ(),
		"MODEL_SUPERVISOR_ADDR=127.0.0.1:0",
		"MODEL_SUPERVISOR_LOCAL_CORE=1",
		"MODEL_SUPERVISOR_TOKEN="+key,
		"MODEL_SUPERVISOR_STATE="+filepath.Join(s.profile, "model-supervisor-state.json"),
		"MODEL_SUPERVISOR_BIN_DIR="+filepath.Join(filepath.Dir(s.binary), "resources", "llama-runtime"),
		"MODEL_SUPERVISOR_MODEL_DIR="+s.modelDirectory("MODEL_SUPERVISOR_MODEL_DIR", "models"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(log, err)
		return
	}
	defer stdout.Close()
	err = runChild(ctx, cmd, func(child context.Context) {
		address := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stdout)
			scanner.Scan()
			address <- scanner.Text()
		}()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-child.Done():
		case <-timer.C:
			fmt.Fprintln(log, "Desktop timed out waiting for the model supervisor listener")
			cancel()
		case line := <-address:
			target, err := url.Parse(line)
			if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" ||
				target.Port() == "" || target.Port() == "0" || target.User != nil {
				fmt.Fprintln(log, "model supervisor did not report a local listener")
				cancel()
				return
			}
			s.proxy.Store(modelSupervisorProxy(target, key))
		}
	})
	if err != nil {
		fmt.Fprintln(log, err)
	}
}

func (s *ModelSupervisor) modelDirectory(envName, fallback string) string {
	path := os.Getenv(envName)
	if path == "" {
		path = fallback
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.profile, path)
}

// ServeHTTP is mounted only in the packaged Wails asset server. The Core HTTP
// listener has no model management proxy, and the bearer stays in Go.
func (s *ModelSupervisor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(supervisorVersionHeader) != "1" {
		http.Error(w, "model supervisor protocol required", http.StatusForbidden)
		return
	}
	proxy := s.proxy.Load()
	if proxy == nil {
		http.Error(w, "model supervisor unavailable", http.StatusServiceUnavailable)
		return
	}
	proxy.ServeHTTP(w, r)
}

func modelSupervisorProxy(target *url.URL, token string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: &http.Transport{ResponseHeaderTimeout: 75 * time.Second},
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header = http.Header{}
			r.Out.Header.Set("Content-Type", "application/json")
			r.Out.Header.Set(supervisorVersionHeader, "1")
			r.Out.Header.Set("X-API-Key", token)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "model supervisor unavailable", http.StatusServiceUnavailable)
		},
	}
}
