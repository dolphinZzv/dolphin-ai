package agentmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"dolphin/internal/config"
)

// AgentSpec describes a child agent to spawn.
type AgentSpec struct {
	Name         string
	Capabilities []string
	Model        string            // empty = inherit parent
	Workspace    string            // empty = temp dir
	MaxRounds    int               // 0 = inherit
	AllowedTools []string
	DeniedTools  []string
	Env          map[string]string
	Timeout      time.Duration // spawn readiness timeout, default 10s
}

// AgentHandle is a reference to a spawned child process.
type AgentHandle struct {
	ID       string
	Spec     AgentSpec
	Card     AgentCard
	Cmd      *exec.Cmd
	WorkDir  string
	HealthCh chan AgentStatus
	mu       sync.Mutex
	status   AgentStatus
}

// Status returns the last known status of the spawned agent.
func (h *AgentHandle) Status() AgentStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}

func (h *AgentHandle) setStatus(s AgentStatus) {
	h.mu.Lock()
	h.status = s
	h.mu.Unlock()
	select {
	case h.HealthCh <- s:
	default:
	}
}

// Spawner launches child Dolphin processes with dynamically-generated configs.
//
// Phase 2: it derives a child config.yaml from the parent's config (deep-copied
// + overridden), execs the dolphin binary, waits for the child to print its
// listen address on stdout, then registers the child with the AgentMesh.
type Spawner struct {
	binPath string          // dolphin binary path
	baseCfg *config.Config  // parent config (template)
	logger  *zap.Logger

	mu      sync.Mutex
	spawned map[string]*AgentHandle // id → handle
	max     int
}

// NewSpawner builds a Spawner. binPath empty → auto-detect via os.Executable.
func NewSpawner(binPath string, baseCfg *config.Config, max int, logger *zap.Logger) *Spawner {
	if logger == nil {
		logger = zap.NewNop()
	}
	if max <= 0 {
		max = 5
	}
	if binPath == "" {
		if exe, err := os.Executable(); err == nil {
			binPath = exe
		}
	}
	return &Spawner{
		binPath: binPath,
		baseCfg: baseCfg,
		logger:  logger,
		spawned: map[string]*AgentHandle{},
		max:     max,
	}
}

// SpawnAndDelegate spawns a child agent and delegates a task to it. The child
// is killed and cleaned up after the delegation completes (or times out).
func (m *AgentMesh) SpawnAndDelegate(ctx context.Context, spec AgentSpec, task string) (*DelegateResult, error) {
	if m.spawner == nil {
		return nil, &DelegateError{Code: ErrInternal, Message: "spawner not configured"}
	}
	handle, err := m.spawner.Spawn(ctx, spec)
	if err != nil {
		return nil, &DelegateError{Code: ErrInternal, Message: "spawn failed", Cause: err.Error()}
	}
	defer m.spawner.Kill(handle)

	// Register then delegate.
	m.Register(handle.Card)
	defer m.Deregister(handle.Card.Name)

	payload := DelegatePayload{
		Task:             task,
		ParentSessionID:  sessionIDFromCtx(ctx),
		DelegationDepth:  depthFromCtx(ctx) + 1,
		PreferredAgent:   handle.Card.Name,
		Timeout:          "0",
	}
	return m.Delegate(ctx, payload)
}

// Spawn starts a child process and waits for it to report its listen address.
func (s *Spawner) Spawn(ctx context.Context, spec AgentSpec) (*AgentHandle, error) {
	s.mu.Lock()
	if len(s.spawned) >= s.max {
		s.mu.Unlock()
		return nil, fmt.Errorf("spawner: max_spawned (%d) reached", s.max)
	}
	s.mu.Unlock()

	if spec.Name == "" {
		spec.Name = "spawned-" + xid.New().String()
	}
	if spec.Workspace == "" {
		spec.Workspace = filepath.Join(os.TempDir(), "dolphin-spawn", spec.Name)
	}
	if err := os.MkdirAll(spec.Workspace, 0o755); err != nil {
		return nil, fmt.Errorf("spawner: mkdir: %w", err)
	}
	cfgPath, err := s.generateConfig(spec)
	if err != nil {
		return nil, err
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(rctx, s.binPath, "--config", cfgPath)
	cmd.Env = append(os.Environ(), envSlice(spec.Env)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("spawner: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawner: start: %w", err)
	}

	listenAddr, err := readListenAddr(stdout, rctx)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("spawner: child did not report listen addr: %w", err)
	}

	card := AgentCard{
		Name:         spec.Name,
		Addr:         listenAddr,
		Capabilities: spec.Capabilities,
		Status:       AgentRunning,
		MaxLoad:      1,
		ProtoVersion: localProto,
	}
	handle := &AgentHandle{
		ID:       xid.New().String(),
		Spec:     spec,
		Card:     card,
		Cmd:      cmd,
		WorkDir:  spec.Workspace,
		HealthCh: make(chan AgentStatus, 4),
	}
	handle.setStatus(AgentRunning)

	s.mu.Lock()
	s.spawned[handle.ID] = handle
	s.mu.Unlock()

	// Reap the process when it exits.
	go func() {
		_ = cmd.Wait()
		handle.setStatus(AgentStopped)
	}()
	return handle, nil
}

// Kill sends SIGTERM, waits a grace period, then SIGKILL. It removes the
// temporary work directory.
func (s *Spawner) Kill(h *AgentHandle) error {
	if h == nil || h.Cmd == nil || h.Cmd.Process == nil {
		return nil
	}
	_ = h.Cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = h.Cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = h.Cmd.Process.Kill()
		<-done
	}
	s.mu.Lock()
	delete(s.spawned, h.ID)
	s.mu.Unlock()
	if h.WorkDir != "" {
		_ = os.RemoveAll(h.WorkDir)
	}
	return nil
}

// Spawned returns the current set of live spawned handles.
func (s *Spawner) Spawned() []*AgentHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*AgentHandle, 0, len(s.spawned))
	for _, h := range s.spawned {
		out = append(out, h)
	}
	return out
}

// generateConfig writes a child config.yaml derived from the parent config
// with the spec's overrides applied, and returns its path.
func (s *Spawner) generateConfig(spec AgentSpec) (string, error) {
	values := s.cloneParentValues()

	// ── must override ──
	values["agent.name"] = spec.Name
	values["agent.workspace"] = spec.Workspace
	values["agent.pool_size"] = 1
	// isolated dirs
	values["session.dir"] = filepath.Join(spec.Workspace, "sessions")
	values["log.file"] = filepath.Join(spec.Workspace, "dolphin.log")
	values["log.compress"] = false
	// no TUI for spawned children
	values["tui.enabled"] = false
	// leaf node: no further delegation/spawn
	values["agents.enabled"] = false
	values["agents.spawner.enabled"] = false
	// random port
	values["agents.listen_addr"] = ":0"

	if spec.Model != "" {
		values["llm.default_model"] = spec.Model
	}
	if spec.MaxRounds > 0 {
		values["agent.max_rounds"] = spec.MaxRounds
	}
	if len(spec.AllowedTools) > 0 {
		values["tool.allowed_tools"] = spec.AllowedTools
	}
	if len(spec.DeniedTools) > 0 {
		// append to existing permission.deny
		existing, _ := values["permission.deny"].([]any)
		merged := append([]any{}, existing...)
		for _, d := range spec.DeniedTools {
			merged = append(merged, d)
		}
		values["permission.deny"] = merged
	}

	// Marshal flat dot-notation map to YAML. dolphin's LoadConfig flattens
	// nested YAML the same way, so flat keys round-trip correctly.
	data, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("spawner: marshal config: %w", err)
	}
	header := fmt.Sprintf("# generated by spawner for %s at %s\n", spec.Name, time.Now().Format(time.RFC3339))
	cfgPath := filepath.Join(spec.Workspace, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(header+string(data)), 0o644); err != nil {
		return "", fmt.Errorf("spawner: write config: %w", err)
	}
	return cfgPath, nil
}

// cloneParentValues returns a deep copy of the parent config's values map.
func (s *Spawner) cloneParentValues() map[string]any {
	out := map[string]any{}
	if s.baseCfg == nil {
		return out
	}
	for _, k := range s.baseCfg.Keys() {
		v := s.baseCfg.Get(k)
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue deep-copies slices/maps so child overrides don't mutate parent.
func cloneValue(v any) any {
	switch x := v.(type) {
	case []any:
		c := make([]any, len(x))
		for i := range x {
			c[i] = cloneValue(x[i])
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(x))
		for k, val := range x {
			c[k] = cloneValue(val)
		}
		return c
	}
	return v
}

// readListenAddr scans the child's stdout for a JSON line reporting its
// listen address: {"listen_addr": ":58472", "pid": 12345}.
func readListenAddr(r interface{ Read(p []byte) (int, error) }, ctx context.Context) (string, error) {
	// Line-buffered read with deadline via ctx.
	type result struct {
		addr string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				// scan complete lines
				for {
					nl := strings.IndexByte(string(buf), '\n')
					if nl < 0 {
						break
					}
					line := string(buf[:nl])
					buf = buf[nl+1:]
					var msg map[string]any
					if json.Unmarshal([]byte(line), &msg) == nil {
						if addr, ok := msg["listen_addr"].(string); ok && addr != "" {
							ch <- result{addr: addr}
							return
						}
					}
				}
			}
			if err != nil {
				ch <- result{err: err}
				return
			}
		}
	}()
	select {
	case res := <-ch:
		return res.addr, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
