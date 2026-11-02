package modelsupervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// helperEnv marks a re-executed test binary that stands in for llama-server, so
// the manager's process control can be exercised without the real runtime.
const helperEnv = "MODELSUPERVISOR_TEST_HELPER"

func TestMain(m *testing.M) {
	if code := os.Getenv("MODELSUPERVISOR_TEST_EXIT"); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			n = 1
		}
		os.Exit(n)
	}
	// A stand-in for llama-server refusing to load a model: it says why on
	// stderr and then exits non-zero.
	if os.Getenv("MODELSUPERVISOR_TEST_FAIL") == "1" {
		fmt.Fprintln(os.Stderr, "llama_model_load: error loading model: failed to load model")
		fmt.Fprintln(os.Stderr, "llama_init_from_gpt_params: failed to load model")
		os.Exit(3)
	}
	if os.Getenv(helperEnv) == "1" {
		fmt.Println("helper-started")
		fmt.Fprintln(os.Stderr, "helper-stderr")
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperLaunch(argv []string) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	return cmd, nil
}

// failingLaunch stands in for a llama-server that exits before it can serve.
func failingLaunch(argv []string) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MODELSUPERVISOR_TEST_FAIL=1")
	return cmd, nil
}

// noPortCheck stands in for the real bind in tests that are not about ports:
// binding a fixed port would make the suite depend on the host's listeners.
func noPortCheck(int) error { return nil }

func newTestManager(t *testing.T) (*Manager, *Store) {
	t.Helper()
	// The default port check binds a real socket, which would make every test
	// depend on whatever the host happens to be listening on — the suite failed
	// on this machine only because a live llama-server held 8081. Tests that
	// exercise the check pass their own seam; checkPortFree has its own test.
	return newTestManagerWith(t, helperLaunch, noPortCheck)
}

// newTestManagerWith builds a manager over a temp model tree. launch and
// portCheck are seams: the real ones need a llama-server binary and a genuinely
// occupied port.
func newTestManagerWith(t *testing.T, launch Launcher, portCheck func(int) error) (*Manager, *Store) {
	t.Helper()
	root := t.TempDir()
	binaryDir := filepath.Join(root, "bin")
	modelDir := filepath.Join(root, "models")
	if err := os.MkdirAll(binaryDir, 0o700); err != nil {
		t.Fatalf("binary dir: %v", err)
	}
	if err := os.MkdirAll(modelDir, 0o700); err != nil {
		t.Fatalf("model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, validSpec().Model), []byte("gguf"), 0o600); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	store := NewStore(filepath.Join(root, "state.json"))
	manager, err := NewManager(Options{
		Store: store, BinaryDir: binaryDir, ModelDir: modelDir,
		Launch: launch, StopTimeout: 5 * time.Second, PortCheck: portCheck,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager, store
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestManagerStartAndStopLifecycle(t *testing.T) {
	manager, _ := newTestManager(t)
	view, err := manager.Start(validSpec())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if view.Status != StatusRunning || view.PID == 0 || view.StartedAt == nil {
		t.Fatalf("view=%+v", view)
	}
	waitFor(t, "captured stdout", func() bool {
		lines, _, _ := manager.Logs("embedding", 0)
		for _, line := range lines {
			if strings.Contains(line.Content, "helper-started") {
				return true
			}
		}
		return false
	})
	if err := manager.Stop("embedding"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	stopped, err := manager.Get("embedding")
	if err != nil || stopped.Status != StatusStopped {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
}

// A7: concurrent starts for one id must produce exactly one winner.
func TestManagerRejectsDuplicateConcurrentStart(t *testing.T) {
	manager, _ := newTestManager(t)
	const workers = 8
	var wg sync.WaitGroup
	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = manager.Start(validSpec())
		}(i)
	}
	wg.Wait()
	wins, duplicates := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrDuplicateID):
			duplicates++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || duplicates != workers-1 {
		t.Fatalf("wins=%d duplicates=%d", wins, duplicates)
	}
}

func TestManagerRejectsPortCollisionAcrossInstances(t *testing.T) {
	manager, _ := newTestManager(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	second := validSpec()
	second.ID = "embedding-two"
	if _, err := manager.Start(second); !errors.Is(err, ErrDuplicatePort) {
		t.Fatalf("got %v, want ErrDuplicatePort", err)
	}
}

func TestManagerRejectsStartWithoutModelFile(t *testing.T) {
	manager, _ := newTestManager(t)
	spec := validSpec()
	spec.Model = "absent.gguf"
	if _, err := manager.Start(spec); err == nil {
		t.Fatal("start without model file accepted")
	}
	if _, err := manager.Get(spec.ID); !errors.Is(err, ErrUnknownInstance) {
		t.Fatalf("failed start left an instance behind: %v", err)
	}
}

func TestManagerStopIsIdempotentAndRejectsUnknown(t *testing.T) {
	manager, _ := newTestManager(t)
	if err := manager.Stop("missing"); !errors.Is(err, ErrUnknownInstance) {
		t.Fatalf("unknown stop: %v", err)
	}
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := manager.Stop("embedding"); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := manager.Stop("embedding"); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestManagerPersistsIntentAndRestoresStopped(t *testing.T) {
	manager, store := newTestManager(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	state, err := store.Load()
	if err != nil || len(state.Instances) != 1 || state.Instances[0].ID != "embedding" {
		t.Fatalf("state=%+v err=%v", state, err)
	}

	restored, err := NewManager(Options{Store: store, BinaryDir: filepath.Dir(store.Path()), ModelDir: filepath.Dir(store.Path()), Launch: helperLaunch, PortCheck: noPortCheck})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := restored.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	views := restored.List()
	if len(views) != 1 || views[0].Status != StatusStopped || views[0].PID != 0 {
		t.Fatalf("restored=%+v", views)
	}
	// The endpoint is derived from the port, never cached per instance: a cached
	// copy is zero-valued on the restore path, so /list would answer with an
	// empty address for every instance after a supervisor restart.
	if got, want := views[0].Endpoint.URL, "http://127.0.0.1:8081/v1"; got != want {
		t.Fatalf("restored endpoint = %q, want %q", got, want)
	}
}

func TestManagerListIsOrderedAndEmptyByDefault(t *testing.T) {
	manager, _ := newTestManager(t)
	if views := manager.List(); len(views) != 0 {
		t.Fatalf("expected empty list, got %+v", views)
	}
	rerank := Spec{ID: "rerank", Model: validSpec().Model, TaskType: TaskRerank, Port: 8082, CtxSize: 4096, Threads: 8}
	if _, err := manager.Start(rerank); err != nil {
		t.Fatalf("start rerank: %v", err)
	}
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start embedding: %v", err)
	}
	views := manager.List()
	if len(views) != 2 || views[0].ID != "embedding" || views[1].ID != "rerank" {
		t.Fatalf("list not ordered: %+v", views)
	}
}

func TestManagerReportsExitCodeOfUnexpectedExit(t *testing.T) {
	root := t.TempDir()
	modelDir := filepath.Join(root, "models")
	if err := os.MkdirAll(modelDir, 0o700); err != nil {
		t.Fatalf("model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, validSpec().Model), []byte("gguf"), 0o600); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	manager, err := NewManager(Options{
		Store:     NewStore(filepath.Join(root, "state.json")),
		BinaryDir: root,
		ModelDir:  modelDir,
		Launch: func([]string) (*exec.Cmd, error) {
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "MODELSUPERVISOR_TEST_EXIT=3")
			return cmd, nil
		},
		PortCheck: noPortCheck,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "failed status", func() bool {
		view, err := manager.Get("embedding")
		return err == nil && view.Status == StatusFailed
	})
	view, err := manager.Get("embedding")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.ExitCode == nil || view.LastError == "" {
		t.Fatalf("exit not recorded: %+v", view)
	}
}

// A persisted instance is restored as stopped. Start must accept it, otherwise a
// supervisor restart would permanently strand every configured instance behind a
// duplicate-id conflict.
func TestStartAcceptsPersistedInstanceAfterReload(t *testing.T) {
	manager, store := newTestManager(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	root := filepath.Dir(store.Path())
	reloaded, err := NewManager(Options{
		Store:       store,
		BinaryDir:   filepath.Join(root, "bin"),
		ModelDir:    filepath.Join(root, "models"),
		Launch:      helperLaunch,
		StopTimeout: 5 * time.Second,
		PortCheck:   noPortCheck,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	view, err := reloaded.Get(validSpec().ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Status != StatusStopped {
		t.Fatalf("restored status = %q, want stopped", view.Status)
	}
	if _, err := reloaded.Start(validSpec()); err != nil {
		t.Fatalf("start after reload: %v", err)
	}
}

// A start that dies must say why. llama-server writes the reason to stderr and
// exits, so an exit code alone leaves the operator with nothing to act on.
func TestFailedStartCarriesLastOutput(t *testing.T) {
	manager, _ := newTestManagerWith(t, failingLaunch, noPortCheck)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "failed state", func() bool {
		view, err := manager.Get(validSpec().ID)
		return err == nil && view.Status == StatusFailed
	})
	view, err := manager.Get(validSpec().ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.ExitCode == nil || *view.ExitCode != 3 {
		t.Fatalf("exit code=%v, want 3", view.ExitCode)
	}
	if !strings.Contains(view.LastError, "failed to load model") {
		t.Fatalf("last error dropped the reason: %q", view.LastError)
	}
}

// A bound port must be refused before a process is spawned, rather than
// returning success and turning into "failed" a second later.
func TestStartRefusesOccupiedPort(t *testing.T) {
	portCheck := func(int) error { return fmt.Errorf("%w: address already in use", ErrPortInUse) }
	manager, _ := newTestManagerWith(t, helperLaunch, portCheck)
	if _, err := manager.Start(validSpec()); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("start: got %v, want ErrPortInUse", err)
	}
	if _, err := manager.Get(validSpec().ID); !errors.Is(err, ErrUnknownInstance) {
		t.Fatalf("a refused start left an instance behind: %v", err)
	}
}

// The production check must actually notice a listening socket.
func TestCheckPortFreeSeesBoundSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := checkPortFree(port); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("checkPortFree(%d): got %v, want ErrPortInUse", port, err)
	}
}

// Observation keeps /list from going stale, but it must never rewrite lifecycle
// state: the state machine follows the process, not a timer.
func TestObserveRecordsHealthWithoutTouchingStatus(t *testing.T) {
	manager, _ := newTestManager(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	prober := &Prober{Dial: func(string) error { return errors.New("nothing is listening") }}
	manager.Observe(context.Background(), prober)

	view, err := manager.Get(validSpec().ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Status != StatusRunning {
		t.Fatalf("status=%q, want running", view.Status)
	}
	if view.Health == nil {
		t.Fatal("observation was not recorded")
	}
	if view.Health.Reachability != ReachUnverified || view.Health.Note == "" {
		t.Fatalf("health=%+v", view.Health)
	}
	if view.Health.CheckedAt.IsZero() {
		t.Fatal("health carries no timestamp, so a reader cannot tell it is stale")
	}
}

func TestObserveSkipsStoppedInstances(t *testing.T) {
	manager, _ := newTestManager(t)
	if _, err := manager.Start(validSpec()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := manager.Stop(validSpec().ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	manager.Observe(context.Background(), &Prober{Dial: func(string) error {
		t.Error("a stopped instance was probed")
		return errors.New("unexpected")
	}})
}
