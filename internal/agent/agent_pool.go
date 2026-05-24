package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"dolphin/internal/config"
	ctxpkg "dolphin/internal/context"
	"dolphin/internal/mcp"
	"dolphin/internal/mcp/shell"
	"dolphin/internal/session"

	"dolphin/internal/agent/compressor"

	"go.uber.org/zap"
)

// AgentInstance is a live agent managed by the pool.
type AgentInstance struct {
	Def   *AgentDef
	Kind  AgentKind
	agent *Agent
	pool  *AgentPool

	mu              sync.RWMutex
	createdAt       time.Time
	status          string // idle / busy / error
	tasksDone       int
	lastTaskAt      time.Time
	taskCh          chan Task
	cancel          context.CancelFunc // cancels the current task
	currentTaskID   string             // task ID currently being processed
	doneCh          chan struct{}      // closed when the worker goroutine exits
	taskChCloseOnce sync.Once
}

func (inst *AgentInstance) Status() string {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.status
}

func (inst *AgentInstance) TasksDone() int {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.tasksDone
}

func (inst *AgentInstance) LastTaskAt() time.Time {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.lastTaskAt
}

// AgentPool manages a pool of agent instances, each with its own goroutine.
type AgentPool struct {
	cfg      PoolConfig
	agents   map[string]*AgentInstance
	resultCh chan TaskResult
	sem      chan struct{} // max concurrency semaphore
	mu       sync.RWMutex
	wg       sync.WaitGroup

	coordinatorCtx  context.Context
	cancel          context.CancelFunc
	parentSessionID session.SessionID
}

// PoolConfig holds pool configuration (subset of config.PoolConfig for decoupling).
type PoolConfig struct {
	MaxConcurrency      int
	DefaultTimeout      int
	WorkspaceDir        string
	IdleTimeout         time.Duration
	MaxPendingResults   int
	MaxPendingResultLen int // chars per result in prompt, 0 = no truncation
}

// NewPoolConfigFromConfig converts a config.PoolConfig to the agent-level
// PoolConfig, handling type conversions (e.g. IdleTimeout int→time.Duration).
func NewPoolConfigFromConfig(cfg config.PoolConfig) PoolConfig {
	return PoolConfig{
		MaxConcurrency:      cfg.MaxConcurrency,
		DefaultTimeout:      cfg.DefaultTimeout,
		WorkspaceDir:        cfg.WorkspaceDir,
		IdleTimeout:         time.Duration(cfg.IdleTimeout) * time.Second,
		MaxPendingResults:   cfg.MaxPendingResults,
		MaxPendingResultLen: cfg.MaxPendingResultLen,
	}
}

func NewAgentPool(ctx context.Context, cfg PoolConfig) *AgentPool {
	ctx, cancel := context.WithCancel(ctx)
	p := &AgentPool{
		cfg:            cfg,
		agents:         make(map[string]*AgentInstance),
		resultCh:       make(chan TaskResult, 128),
		sem:            make(chan struct{}, cfg.MaxConcurrency),
		coordinatorCtx: ctx,
		cancel:         cancel,
	}

	// Start idle reaper for coordinator-created agents
	if cfg.IdleTimeout > 0 {
		go p.reapIdleAgents()
	}

	return p
}

// SetParentSessionID sets the parent session ID for tracing sub-agent sessions.
// Must be called before any tasks are dispatched.
func (p *AgentPool) SetParentSessionID(id session.SessionID) {
	p.parentSessionID = id
}

// ParentSessionID returns the current parent session ID.
func (p *AgentPool) ParentSessionID() session.SessionID { return p.parentSessionID }

// Add registers a new agent in the pool and starts its worker goroutine.
func (p *AgentPool) Add(name string, def *AgentDef, kind AgentKind, agent *Agent, tools *mcp.Registry) *AgentInstance {
	// Build filtered tool registry
	filteredTools := tools.FilteredView(def.Tools)

	// Wrap skill/workflow tool handlers with agent-specific visibility
	if len(def.Skills) > 0 {
		filteredTools = wrapSkillTools(filteredTools, def.Skills)
	}
	if len(def.Workflows) > 0 {
		filteredTools = wrapWorkflowTools(filteredTools, def.Workflows)
	}

	// Create a sub-agent with the filtered tools
	comp := agent.compressor
	if comp == nil {
		comp = &compressor.DropCompressor{}
	}
	subAgent := &Agent{
		cfg:        agent.cfg.Clone(),
		sessMgr:    agent.sessMgr,
		toolReg:    filteredTools,
		provider:   agent.provider,
		ctxBuilder: agent.ctxBuilder,
		compressor: comp,
	}

	taskCh := make(chan Task, 8)
	inst := &AgentInstance{
		Def:       def,
		Kind:      kind,
		agent:     subAgent,
		pool:      p,
		createdAt: time.Now(),
		status:    "idle",
		taskCh:    taskCh,
		doneCh:    make(chan struct{}),
	}

	p.mu.Lock()
	p.agents[name] = inst
	size := int64(len(p.agents))
	p.mu.Unlock()
	agentPoolSize.Add(1)
	if TelemetryCallbacks.OnPoolSize != nil {
		TelemetryCallbacks.OnPoolSize(size)
	}

	// Start worker goroutine
	p.wg.Add(1)
	go p.workerLoop(inst, taskCh)

	zap.S().Infow("agent added to pool",
		"name", name,
		"kind", kind,
		"tools", def.Tools,
		"workspace", def.Workspace,
	)
	return inst
}

// workerLoop processes tasks from the agent's channel.
func (p *AgentPool) workerLoop(inst *AgentInstance, taskCh <-chan Task) {
	defer p.wg.Done()
	defer close(inst.doneCh)
	defer func() {
		if r := recover(); r != nil {
			zap.S().Errorw("agent panic recovered",
				"name", inst.Def.Name,
				"recover", fmt.Sprintf("%v", r),
			)
			inst.mu.Lock()
			inst.status = "error"
			inst.mu.Unlock()
			select {
			case p.resultCh <- TaskResult{
				AgentName: inst.Def.Name,
				Success:   false,
				Status:    "error",
				Error:     fmt.Sprintf("panic: %v", r),
			}:
			default:
				zap.S().Warnw("result channel closed, dropping panic result",
					"name", inst.Def.Name,
				)
			}
		}
	}()

	for {
		select {
		case <-p.coordinatorCtx.Done():
			return
		case task, ok := <-taskCh:
			if !ok {
				return
			}
			p.processTask(inst, task)
		}
	}
}

// processTask runs a single task on the agent.
func (p *AgentPool) processTask(inst *AgentInstance, task Task) {
	// Acquire concurrency slot
	select {
	case p.sem <- struct{}{}:
	case <-p.coordinatorCtx.Done():
		return
	}
	defer func() { <-p.sem }()

	inst.mu.Lock()
	inst.status = "busy"
	inst.tasksDone++
	inst.currentTaskID = task.ID
	inst.mu.Unlock()
	activeAgents.Add(1)
	if TelemetryCallbacks.OnActiveAgents != nil {
		TelemetryCallbacks.OnActiveAgents(activeAgents.Value())
	}

	// Create task context with timeout
	timeout := task.Timeout
	if timeout <= 0 {
		timeout = p.cfg.DefaultTimeout
	}
	taskCtx, cancel := context.WithTimeout(p.coordinatorCtx, time.Duration(timeout)*time.Second)

	// Set workspace directory for workspace isolation
	if inst.Def.Workspace != "" {
		taskCtx = shell.WithWorkdir(taskCtx, inst.Def.Workspace)
	}

	// Store cancel function so CancelTask can call it
	inst.mu.Lock()
	inst.cancel = cancel
	inst.mu.Unlock()

	// Ensure cleanup
	defer func() {
		cancel()
		activeAgents.Add(-1)
		if TelemetryCallbacks.OnActiveAgents != nil {
			TelemetryCallbacks.OnActiveAgents(activeAgents.Value())
		}
		inst.mu.Lock()
		inst.cancel = nil
		inst.status = "idle"
		inst.currentTaskID = ""
		inst.lastTaskAt = time.Now()
		inst.mu.Unlock()
	}()

	// Build system prompt for this agent
	inst.agent.ctxBuilder.SetRenderData(ctxpkg.NewRenderData(inst.agent.cfg))
	systemPrompt, err := inst.agent.ctxBuilder.BuildForAgent(inst.Def.Name)
	if err != nil {
		zap.S().Errorw("build agent context failed", "agent", inst.Def.Name, "error", err)
		systemPrompt = "You are a helpful assistant."
	}

	zap.S().Debugw("agent processing task",
		"agent", inst.Def.Name,
		"task_id", task.ID,
		"timeout", timeout,
	)

	var taskEnd func()
	if TelemetryCallbacks.OnTaskSpan != nil {
		taskEnd = TelemetryCallbacks.OnTaskSpan(taskCtx, inst.Def.Name, task.ID)
	}
	result, err := inst.agent.RunTask(taskCtx, task.Input, systemPrompt, inst.agent.toolReg, p.parentSessionID)
	if taskEnd != nil {
		taskEnd()
	}
	if err != nil {
		zap.S().Debugw("agent task finished with error",
			"agent", inst.Def.Name,
			"task_id", task.ID,
			"error", err,
		)
	}
	result.AgentName = inst.Def.Name
	result.TaskID = task.ID

	// Track task result
	if result.Success {
		taskCompleted.Inc()
	} else {
		taskFailed.Inc()
	}
	if TelemetryCallbacks.OnTaskCompleted != nil {
		TelemetryCallbacks.OnTaskCompleted(inst.Def.Name, result.Success)
	}

	// If context was cancelled externally, override status
	if taskCtx.Err() != nil && result.Status == "completed" {
		result.Status = "cancelled"
		result.Success = false
		result.Error = taskCtx.Err().Error()
	}

	select {
	case p.resultCh <- result:
	default:
		zap.S().Errorw("result channel full, dropping result", "agent", inst.Def.Name, "task_id", task.ID)
	}
}

// Dispatch sends a task to an agent. Returns the task ID if accepted.
func (p *AgentPool) Dispatch(agentName string, task Task) error {
	p.mu.RLock()
	inst, ok := p.agents[agentName]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	select {
	case inst.taskCh <- task:
		taskDispatched.Inc()
		if TelemetryCallbacks.OnTaskDispatched != nil {
			TelemetryCallbacks.OnTaskDispatched(agentName)
		}
		if TelemetryCallbacks.OnDispatchSpan != nil {
			end := TelemetryCallbacks.OnDispatchSpan(context.Background(), agentName)
			if end != nil {
				end()
			}
		}
		return nil
	default:
		return fmt.Errorf("agent %s task queue full", agentName)
	}
}

// Collect drains all completed task results (non-blocking).
func (p *AgentPool) Collect() []TaskResult {
	var results []TaskResult
	for {
		select {
		case r, ok := <-p.resultCh:
			if !ok {
				return results
			}
			results = append(results, r)
		default:
			return results
		}
	}
}

// Cancel cancels a specific task by ID. Returns true if a matching running
// task was found and cancelled.
func (p *AgentPool) Cancel(taskID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, inst := range p.agents {
		inst.mu.RLock()
		cancel := inst.cancel
		tid := inst.currentTaskID
		inst.mu.RUnlock()
		if cancel != nil && tid == taskID {
			cancel()
			return true
		}
	}
	return false
}

// CancelAll cancels all currently running tasks.
func (p *AgentPool) CancelAll() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, inst := range p.agents {
		inst.mu.RLock()
		cancel := inst.cancel
		inst.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
	}
}

// List returns status of all agents.
func (p *AgentPool) List() []AgentStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	list := make([]AgentStatus, 0, len(p.agents))
	for name, inst := range p.agents {
		inst.mu.RLock()
		s := AgentStatus{
			Name:      name,
			Kind:      inst.Kind.String(),
			Role:      inst.Def.Role,
			Status:    inst.status,
			TasksDone: inst.tasksDone,
			Workspace: inst.Def.Workspace,
		}
		inst.mu.RUnlock()
		list = append(list, s)
	}
	return list
}

// cleanWorkspace removes the workspace directory for coordinator-created agents.
func (p *AgentPool) cleanWorkspace(inst *AgentInstance) {
	if inst.Kind != AgentCoord || inst.Def.Workspace == "" {
		return
	}
	if err := os.RemoveAll(inst.Def.Workspace); err != nil {
		zap.S().Warnw("failed to clean temp workspace", "name", inst.Def.Name, "workspace", inst.Def.Workspace, "error", err)
	} else {
		zap.S().Debugw("cleaned temp workspace", "name", inst.Def.Name, "workspace", inst.Def.Workspace)
	}
}

// Remove removes an agent by name, cancels any running task, waits for the
// worker to finish, and cleans up its workspace.
func (p *AgentPool) Remove(name string) bool {
	p.mu.Lock()
	inst, ok := p.agents[name]
	if ok {
		delete(p.agents, name)
		agentPoolSize.Add(-1)
		if TelemetryCallbacks.OnPoolSize != nil {
			TelemetryCallbacks.OnPoolSize(int64(len(p.agents)))
		}
	}
	p.mu.Unlock()
	if ok {
		// Cancel any running task first
		inst.mu.Lock()
		if inst.cancel != nil {
			inst.cancel()
		}
		inst.mu.Unlock()

		inst.taskChCloseOnce.Do(func() {
			close(inst.taskCh)
		})

		// Wait for worker to exit (with timeout to prevent deadlock)
		select {
		case <-inst.doneCh:
		case <-time.After(5 * time.Second):
			zap.S().Warnw("timeout waiting for agent worker to exit",
				"name", name,
				"timeout", 5,
			)
		}

		p.cleanWorkspace(inst)
		return true
	}
	return false
}

// Shutdown gracefully stops the pool: cancels all tasks, waits for goroutines, cleans up.
func (p *AgentPool) Shutdown() {
	zap.S().Infow("agent pool shutting down...")
	p.cancel()  // Cancel all tasks
	p.wg.Wait() // Wait for all goroutines
	close(p.resultCh)
	// Clean up all coordinator-created workspaces
	p.mu.Lock()
	for name, inst := range p.agents {
		if inst.Kind == AgentCoord {
			p.cleanWorkspace(inst)
		}
		delete(p.agents, name)
	}
	p.mu.Unlock()
	zap.S().Infow("agent pool shutdown complete")
}

// reapIdleAgents periodically removes idle coordinator-created agents.
// The check interval is proportional to IdleTimeout (1/4th), clamped to
// [5s, 30s] to balance promptness versus overhead.
func (p *AgentPool) reapIdleAgents() {
	interval := p.cfg.IdleTimeout / 4
	if interval > 30*time.Second {
		interval = 30 * time.Second
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.coordinatorCtx.Done():
			return
		case <-ticker.C:
			// Collect idle agents to reap
			p.mu.Lock()
			var reap []*AgentInstance
			for name, inst := range p.agents {
				if inst.Kind == AgentBuildin {
					continue // buildin agents are persistent
				}
				if inst.Kind != AgentCoord {
					continue
				}
				inst.mu.RLock()
				status := inst.status
				lastRun := inst.lastTaskAt
				inst.mu.RUnlock()

				if status != "idle" {
					continue
				}
				if !lastRun.IsZero() && time.Since(lastRun) > p.cfg.IdleTimeout {
					zap.S().Infow("reaping idle coordinator-created agent", "name", name)
					delete(p.agents, name)
					agentPoolSize.Add(-1)
					inst.taskChCloseOnce.Do(func() {
						close(inst.taskCh)
					})
					reap = append(reap, inst)
				}
			}
			if len(reap) > 0 && TelemetryCallbacks.OnPoolSize != nil {
				TelemetryCallbacks.OnPoolSize(int64(len(p.agents)))
			}
			p.mu.Unlock()

			// Wait for workers to exit and clean up (outside the lock)
			for _, inst := range reap {
				select {
				case <-inst.doneCh:
				case <-time.After(5 * time.Second):
					zap.S().Warnw("timeout waiting for reaped agent worker", "name", inst.Def.Name)
				}
				p.cleanWorkspace(inst)
			}
		}
	}
}

// filterTool wraps an mcp.Tool with a pre-check function.
// If preCheck returns a non-empty error message, the tool call is rejected.
type filterTool struct {
	def      mcp.ToolDefinition
	original mcp.Tool
	preCheck func(ctx context.Context, input json.RawMessage) string // empty = allowed
}

func (f *filterTool) Definition() mcp.ToolDefinition { return f.def }

func (f *filterTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if msg := f.preCheck(ctx, input); msg != "" {
		return &mcp.ToolResult{Content: msg, IsError: true}, nil
	}
	return f.original.Execute(ctx, input)
}

// wrapSkillTools replaces load_skill with a filtered version.
// If allowed is empty, returns the registry unchanged.
func wrapSkillTools(reg *mcp.Registry, allowed []string) *mcp.Registry {
	if len(allowed) == 0 {
		return reg
	}
	if t, ok := reg.Get("load_skill"); ok {
		reg.Register(&filterTool{
			def:      t.Definition(),
			original: t,
			preCheck: func(_ context.Context, input json.RawMessage) string {
				var params struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal(input, &params); err != nil {
					return "invalid input: " + err.Error()
				}
				for _, a := range allowed {
					if a == params.Name {
						return ""
					}
				}
				return fmt.Sprintf("Skill %q is not available for this agent. Allowed skills: %v", params.Name, allowed)
			},
		})
	}
	return reg
}

// wrapWorkflowTools replaces load_workflow and run_workflow with filtered versions.
// If allowed is empty, returns the registry unchanged.
func wrapWorkflowTools(reg *mcp.Registry, allowed []string) *mcp.Registry {
	if len(allowed) == 0 {
		return reg
	}
	for _, name := range []string{"load_workflow", "run_workflow"} {
		if t, ok := reg.Get(name); ok {
			reg.Register(&filterTool{
				def:      t.Definition(),
				original: t,
				preCheck: func(_ context.Context, input json.RawMessage) string {
					var params struct {
						Name string `json:"name"`
					}
					if err := json.Unmarshal(input, &params); err != nil {
						return "invalid input: " + err.Error()
					}
					for _, a := range allowed {
						if a == params.Name {
							return ""
						}
					}
					return fmt.Sprintf("Workflow %q is not available for this agent. Allowed workflows: %v", params.Name, allowed)
				},
			})
		}
	}
	return reg
}
