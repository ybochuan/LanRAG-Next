package modelsupervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/modelsupervisor/winjob"
)

// ErrUnsupported reports that this platform cannot supervise processes. The
// supervisor refuses to run rather than degrade: a process started under WSL has
// a Linux PID that no Windows tool can terminate.
var ErrUnsupported = winjob.ErrUnsupported

// defaultStopTimeout bounds how long Stop waits for a process to disappear.
const defaultStopTimeout = 10 * time.Second

// Launcher starts a process for an argv. It exists so tests can drive the state
// machine without a real llama-server binary.
type Launcher func(argv []string) (*exec.Cmd, error)

// Options configures a Manager.
type Options struct {
	Store       *Store
	BinaryDir   string
	ModelDir    string
	APIKey      string
	Job         *winjob.Job
	Launch      Launcher
	Now         func() time.Time
	StopTimeout time.Duration
	LogLines    int
	// PortCheck reports whether a loopback port is free. It is a seam so tests
	// can simulate an occupied port; the real check cannot be faked.
	PortCheck func(port int) error
}

// instance is the runtime half of a model: never persisted, never serialized.
type instance struct {
	spec      Spec
	status    Status
	pid       int
	startedAt time.Time
	lastError string
	exitCode  *int
	logs      *LogBuffer
	cmd       *exec.Cmd
	done      chan struct{}
	health    *ProbeResult
	healthAt  time.Time
}

// allowedTransitions is the whole state machine. Anything absent is a 409.
var allowedTransitions = map[Status]map[Status]bool{
	StatusStopped:  {StatusStarting: true},
	StatusStarting: {StatusRunning: true, StatusFailed: true, StatusStopping: true},
	StatusRunning:  {StatusStopping: true, StatusFailed: true},
	StatusStopping: {StatusStopped: true, StatusFailed: true},
	StatusFailed:   {StatusStarting: true, StatusStopping: true, StatusStopped: true},
}

// Manager owns every instance. One mutex guards the map and the state machine;
// probes and process waits always run outside it, so a slow start cannot block
// an unrelated health check.
type Manager struct {
	mu        sync.Mutex
	opts      Options
	job       *winjob.Job
	instances map[string]*instance
}

// NewManager builds a manager. It requires a store and both roots; it does not
// create any process or touch the filesystem beyond option validation.
func NewManager(opts Options) (*Manager, error) {
	if opts.Store == nil {
		return nil, errors.New("state store required")
	}
	if opts.BinaryDir == "" || opts.ModelDir == "" {
		return nil, errors.New("binary and model directories required")
	}
	if opts.Launch == nil {
		opts.Launch = execLaunch
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = defaultStopTimeout
	}
	if opts.PortCheck == nil {
		opts.PortCheck = checkPortFree
	}
	return &Manager{opts: opts, job: opts.Job, instances: make(map[string]*instance)}, nil
}

// checkPortFree reports whether anything is already listening on the instance
// port. llama-server exits immediately when it cannot bind, which the operator
// otherwise learns only from a "failed" state seconds later; asking first turns
// a confusing failure into an immediate 409.
//
// The listen-and-close window is a TOCTOU hole by construction — the port can be
// taken between the check and the child's bind. It is still worth doing: it
// catches the overwhelmingly common case (a leftover process, another app) and
// costs nothing when it is wrong.
func checkPortFree(port int) error {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPortInUse, err)
	}
	return listener.Close()
}

// Load restores persisted intent as stopped instances. A corrupt state file is
// returned as an error and is never overwritten.
func (m *Manager) Load() error {
	state, err := m.opts.Store.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, spec := range state.Instances {
		m.instances[spec.ID] = &instance{spec: spec, status: StatusStopped, logs: NewLogBuffer(m.opts.LogLines)}
	}
	return nil
}

// Start launches one instance. It returns as soon as the process is created; the
// caller polls or subscribes for the transition to running.
func (m *Manager) Start(spec Spec) (View, error) {
	spec = spec.Normalize()
	if err := spec.Validate(); err != nil {
		return View{}, err
	}
	m.mu.Lock()
	// A persisted instance is restored as stopped so the operator can start it
	// again after a supervisor restart; only a live instance owns its id.
	if existing, exists := m.instances[spec.ID]; exists && existing.status != StatusStopped && existing.status != StatusFailed {
		m.mu.Unlock()
		return View{}, ErrDuplicateID
	}
	for id, existing := range m.instances {
		if id == spec.ID {
			continue
		}
		if existing.spec.Port == spec.Port {
			m.mu.Unlock()
			return View{}, ErrDuplicatePort
		}
	}
	inst := &instance{
		spec: spec, status: StatusStarting, logs: NewLogBuffer(m.opts.LogLines),
		done: make(chan struct{}),
	}
	m.instances[spec.ID] = inst
	m.mu.Unlock()

	// Ask before starting: a bound port makes llama-server exit at once, and the
	// operator should not have to read a log to learn that.
	if err := m.opts.PortCheck(spec.Port); err != nil {
		m.mu.Lock()
		delete(m.instances, spec.ID)
		m.mu.Unlock()
		return View{}, err
	}

	if err := m.launch(inst); err != nil {
		m.mu.Lock()
		delete(m.instances, spec.ID)
		m.mu.Unlock()
		return View{}, err
	}
	// Probe-gated promotion lands in probe.go; until then a live process is the
	// only evidence available.
	m.mu.Lock()
	inst.status = StatusRunning
	m.mu.Unlock()
	m.persist()
	return m.view(spec.ID)
}

// launch performs the filesystem, process and wiring work for one instance. Any
// failure is terminal for that attempt and leaves no partial instance behind.
func (m *Manager) launch(inst *instance) error {
	binary, err := ResolveUnder(m.opts.BinaryDir, BinaryName)
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}
	modelPath, err := ResolveUnder(m.opts.ModelDir, inst.spec.Model)
	if err != nil {
		return fmt.Errorf("resolve model: %w", err)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model %s is not available: %w", inst.spec.Model, err)
	}
	argv, err := inst.spec.Argv(binary, modelPath, m.opts.APIKey)
	if err != nil {
		return err
	}
	cmd, err := m.opts.Launch(argv)
	if err != nil {
		return fmt.Errorf("launch %s: %w", inst.spec.ID, err)
	}
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		writer.Close()
		return fmt.Errorf("start %s: %w", inst.spec.ID, err)
	}
	if m.job != nil {
		if err := m.job.AssignPID(cmd.Process.Pid); err != nil {
			cmd.Process.Kill()
			writer.Close()
			return fmt.Errorf("supervise %s: %w", inst.spec.ID, err)
		}
	}
	drained := make(chan struct{})
	go drain(inst.logs, reader, drained)
	go m.await(inst.spec.ID, cmd, writer, inst.done, drained)

	m.mu.Lock()
	inst.pid = cmd.Process.Pid
	inst.cmd = cmd
	inst.startedAt = m.opts.Now().UTC()
	m.mu.Unlock()
	return nil
}

// drain copies merged stdout/stderr into the bounded log buffer and closes done
// when the pipe reaches EOF.
func drain(logs *LogBuffer, reader io.Reader, done chan struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		logs.Append(scanner.Text())
	}
}

// tailWait bounds how long await waits for the reader to catch up before it
// reports the failure. Without the last lines a start failure is just an exit
// code, and llama-server puts the reason on stderr.
const (
	tailLines    = 3
	tailMaxRunes = 400
	tailWait     = 2 * time.Second
)

// await blocks until the process exits, then records why. It never holds the
// manager lock while waiting.
func (m *Manager) await(id string, cmd *exec.Cmd, writer *io.PipeWriter, done chan struct{}, drained chan struct{}) {
	err := cmd.Wait()
	writer.Close()
	// Let the reader finish first, otherwise the lines that explain the exit are
	// still in the pipe when the reason is composed.
	select {
	case <-drained:
	case <-time.After(tailWait):
	}
	close(done)
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instances[id]
	if !ok {
		return
	}
	if inst.status == StatusStopping {
		inst.status = StatusStopped
		return
	}
	inst.status = StatusFailed
	tail := inst.logs.Tail(tailLines, tailMaxRunes)
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code := exitErr.ExitCode()
		inst.exitCode = &code
		inst.lastError = withTail(fmt.Sprintf("process exited with code %d", code), tail)
	case err != nil:
		inst.lastError = withTail(err.Error(), tail)
	default:
		inst.lastError = withTail("process exited", tail)
	}
}

// withTail appends the captured output to a failure reason.
func withTail(reason, tail string) string {
	if tail == "" {
		return reason
	}
	return reason + ": " + tail
}

// Stop terminates one instance and waits for it to disappear.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	inst, ok := m.instances[id]
	if !ok {
		m.mu.Unlock()
		return ErrUnknownInstance
	}
	switch inst.status {
	case StatusStopped:
		m.mu.Unlock()
		return nil
	case StatusFailed:
		inst.status = StatusStopped
		m.mu.Unlock()
		m.persist()
		return nil
	}
	if !allowedTransitions[inst.status][StatusStopping] {
		current := inst.status
		m.mu.Unlock()
		return fmt.Errorf("%w: %s -> stopping", ErrStateConflict, current)
	}
	inst.status = StatusStopping
	cmd, done := inst.cmd, inst.done
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			m.markFailed(id, fmt.Errorf("kill: %w", err))
			return err
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(m.opts.StopTimeout):
			m.markFailed(id, errors.New("process did not exit before the stop timeout"))
			return ErrStateConflict
		}
	}
	m.mu.Lock()
	inst.status = StatusStopped
	inst.cmd = nil
	m.mu.Unlock()
	m.persist()
	return nil
}

func (m *Manager) markFailed(id string, cause error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[id]; ok {
		inst.status = StatusFailed
		inst.lastError = cause.Error()
	}
}

// List returns every instance ordered by id.
func (m *Manager) List() []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]View, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.viewLocked(id))
	}
	return out
}

// Get returns one instance projection.
func (m *Manager) Get(id string) (View, error) { return m.view(id) }

// Observe probes every running instance and records the result. It is what turns
// reachability from a start-time guess into a measurement with a timestamp: a
// server that answers /health but hangs on its task endpoint, or one that dies
// without the supervisor noticing yet, becomes visible here instead of only when
// an operator clicks refresh.
//
// A probe never changes lifecycle status. Reporting "failed" from a health check
// would let one slow answer tear down an instance that is merely busy; the state
// machine follows the process, and health is reported beside it.
func (m *Manager) Observe(ctx context.Context, prober *Prober) {
	m.mu.Lock()
	running := make([]Spec, 0, len(m.instances))
	for _, inst := range m.instances {
		if inst.status == StatusRunning {
			running = append(running, inst.spec)
		}
	}
	m.mu.Unlock()

	for _, spec := range running {
		if ctx.Err() != nil {
			return
		}
		result := prober.Check(ctx, spec)
		m.mu.Lock()
		if inst, ok := m.instances[spec.ID]; ok && inst.status == StatusRunning {
			captured := result
			inst.health = &captured
			inst.healthAt = m.opts.Now().UTC()
		}
		m.mu.Unlock()
	}
}

// ModelPath resolves a spec's model file and reports its size. It exists so the
// HTTP layer can run the capacity precheck before committing to a start, using
// the same containment rules the launch path uses.
func (m *Manager) ModelPath(spec Spec) (string, int64, error) {
	spec.Normalize()
	if err := spec.Validate(); err != nil {
		return "", 0, err
	}
	path, err := ResolveUnder(m.opts.ModelDir, spec.Model)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("model %s is not available: %w", spec.Model, err)
	}
	return path, info.Size(), nil
}

// Logs returns retained output newer than seq.
func (m *Manager) Logs(id string, seq uint64) ([]LogLine, uint64, error) {
	m.mu.Lock()
	inst, ok := m.instances[id]
	m.mu.Unlock()
	if !ok {
		return nil, 0, ErrUnknownInstance
	}
	lines, head := inst.logs.Since(seq)
	return lines, head, nil
}

// Close stops every instance and releases the job handle, which terminates any
// process still assigned to it.
func (m *Manager) Close() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
	return m.job.Close()
}

func (m *Manager) view(id string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.instances[id]; !ok {
		return View{}, ErrUnknownInstance
	}
	return m.viewLocked(id), nil
}

func (m *Manager) viewLocked(id string) View {
	inst := m.instances[id]
	view := ViewOf(inst.spec)
	view.Status = inst.status
	view.PID = inst.pid
	if inst.health != nil {
		view.Health = &Health{
			Reachability: inst.health.Reachability,
			EmbeddingDim: inst.health.EmbeddingDim,
			LatencyMS:    inst.health.LatencyMS,
			Note:         inst.health.Note,
			CheckedAt:    inst.healthAt,
		}
	}
	view.LastError = inst.lastError
	view.ExitCode = inst.exitCode
	if !inst.startedAt.IsZero() {
		started := inst.startedAt
		view.StartedAt = &started
	}
	return view
}

// persist writes current intent. Persistence failures are recorded on the
// instance rather than aborting a start that already succeeded.
func (m *Manager) persist() {
	m.mu.Lock()
	specs := make([]Spec, 0, len(m.instances))
	for _, inst := range m.instances {
		specs = append(specs, inst.spec)
	}
	m.mu.Unlock()
	if err := m.opts.Store.Save(State{Instances: specs}); err != nil {
		m.mu.Lock()
		for _, inst := range m.instances {
			if inst.lastError == "" {
				inst.lastError = "state not persisted: " + err.Error()
			}
		}
		m.mu.Unlock()
	}
}

// execLaunch is the production launcher.
func execLaunch(argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command line")
	}
	return exec.Command(argv[0], argv[1:]...), nil
}
