package agent

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	ctxpkg "dolphin/internal/context"
	"dolphin/internal/mcp"
	"dolphin/internal/mcp/shell"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"dolphin/internal/agent/compressor"
	"dolphin/internal/agent/provider"

	"go.uber.org/zap"
	"gopkg.in/natefinch/lumberjack.v2"
)

// priorityTask wraps a Task with its priority for the heap.
type priorityTask struct {
	task     Task
	priority Priority
	index    int // index in the heap (maintained by heap.Interface)
}

// priorityQueue implements heap.Interface. Higher priority tasks are popped first.
type priorityQueue []*priorityTask

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].priority > pq[j].priority // higher priority first
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*priorityTask)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// resultQueue is a thread-safe, unbounded queue for TaskResult backed by a slice.
// It uses sync.Cond for event-driven notification so waiters can block efficiently.
type resultQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []TaskResult
	closed bool
}

func newResultQueue() *resultQueue {
	q := &resultQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push adds a result. Returns false if queue is closed or exceeds hard cap (10000).
func (q *resultQueue) push(item TaskResult) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if len(q.items) >= 10000 {
		return false
	}
	q.items = append(q.items, item)
	q.cond.Signal()
	return true
}

// popAll drains all available results (non-blocking).
func (q *resultQueue) popAll() []TaskResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	items := q.items
	q.items = nil
	return items
}

// waitForResult blocks until results are available or timeout expires.
// Returns all accumulated items (may be empty on timeout).
func (q *resultQueue) waitForResult(timeout time.Duration) []TaskResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 && !q.closed {
		go func() {
			time.Sleep(timeout)
			q.cond.Broadcast()
		}()
		q.cond.Wait()
	}

	items := q.items
	q.items = nil
	return items
}

// close marks the queue as closed and wakes all waiters.
func (q *resultQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}

func (q *resultQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// findByID looks up a result by task ID and removes it from the queue.
// Returns nil if not found. Non-blocking.
func (q *resultQueue) findByID(taskID string) *TaskResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, r := range q.items {
		if r.TaskID == taskID {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return &r
		}
	}
	return nil
}

// AgentInstance is a live agent managed by the pool.
type AgentInstance struct {
	Def   *AgentDef
	Kind  AgentKind
	agent *Agent
	pool  *AgentPool

	mu            sync.RWMutex
	createdAt     time.Time
	status        string // idle / busy / error
	tasksDone     int
	lastTaskAt    time.Time
	cancel        context.CancelFunc // cancels the current task
	currentTaskID string             // task ID currently being processed
	sessionID     session.SessionID  // parent coordinator session ID
	doneCh        chan struct{}      // closed when the worker goroutine exits

	// Priority task queue (replaces taskCh)
	taskMu        sync.Mutex
	taskPQ        priorityQueue
	taskCond      *sync.Cond
	taskClosed    bool
	taskCloseOnce sync.Once

	sem chan struct{} // semaphore for this instance (group or global)

	io transport.UserIO // SubAgentIO for this agent (nil if not available)

	// loopState holds persistent conversation state across dispatches
	loopState *LoopState
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

// pushTask adds a task to the agent's priority queue. Returns false if queue is closed.
func (inst *AgentInstance) pushTask(task Task, pri Priority) bool {
	inst.taskMu.Lock()
	defer inst.taskMu.Unlock()
	if inst.taskClosed {
		return false
	}
	heap.Push(&inst.taskPQ, &priorityTask{task: task, priority: pri})
	inst.taskCond.Signal()
	return true
}

// popTask blocks until a task is available or the queue is closed. Returns false if closed.
func (inst *AgentInstance) popTask() (Task, bool) {
	inst.taskMu.Lock()
	defer inst.taskMu.Unlock()
	for len(inst.taskPQ) == 0 && !inst.taskClosed {
		inst.taskCond.Wait()
	}
	if len(inst.taskPQ) == 0 {
		return Task{}, false // closed and empty
	}
	item := heap.Pop(&inst.taskPQ).(*priorityTask)
	return item.task, true
}

// closeTasks marks the queue as closed and wakes all waiters (idempotent).
func (inst *AgentInstance) closeTasks() {
	inst.taskMu.Lock()
	defer inst.taskMu.Unlock()
	inst.taskClosed = true
	inst.taskCond.Broadcast()
}

// Mailbox holds messages for a single agent.
type Mailbox struct {
	mu       sync.Mutex
	cond     *sync.Cond
	messages []AgentMessage
}

func newMailbox() *Mailbox {
	mb := &Mailbox{}
	mb.cond = sync.NewCond(&mb.mu)
	return mb
}

// AgentPool manages a pool of agent instances, each with its own goroutine.
type AgentPool struct {
	cfg       PoolConfig
	agents    map[string]*AgentInstance
	results   *resultQueue
	mailboxes map[string]*Mailbox
	groups    map[string]chan struct{} // per-group semaphores
	sem       chan struct{}            // max concurrency semaphore
	mu        sync.RWMutex
	wg        sync.WaitGroup

	coordinatorCtx  context.Context
	cancel          context.CancelFunc
	parentSessionID session.SessionID

	// OnRemoveAgent is called when an agent is removed from the pool (via Remove or reaper).
	// Used by Coordinator to clean up MultiIO registrations.
	OnRemoveAgent func(name string)
}

// PoolConfig holds pool configuration (subset of config.PoolConfig for decoupling).
type PoolConfig struct {
	MaxConcurrency      int
	DefaultTimeout      int
	WorkspaceDir        string
	IdleTimeout         time.Duration
	MaxPendingResults   int
	MaxPendingResultLen int // chars per result in prompt, 0 = no truncation
	MaxSynthesisRounds  int // cap on coordinator poll synthesis, 0 = default 3
	PollInterval        time.Duration
	MinReapInterval     time.Duration
	MaxReapInterval     time.Duration
	DispatchTimeout     time.Duration  // 0 = no blocking fallback on full task channel
	WorkerStopTimeout   time.Duration  // worker shutdown grace period
	GroupLimits         map[string]int // per-group concurrency caps (empty = use global)
	MaxStaleDuration    time.Duration  // max age for error agents before workspace cleanup (default 1h)
	EnableAgentLog      bool           // write agent execution log to workspace/agent.log
	AgentLogMaxSize     int            // MB before rotation (default 100)
	AgentLogMaxAge      int            // days to retain (default 30)
	AgentLogMaxBackups  int            // max old files (default 3)
	MaxAgentMessages    int            // max conversation messages retained per subagent, 0 = unlimited (default 100)
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
		MaxSynthesisRounds:  cfg.MaxSynthesisRounds,
		PollInterval:        parseDurationOpt(cfg.PollInterval, 200*time.Millisecond),
		MinReapInterval:     parseDurationOpt(cfg.MinReapInterval, 5*time.Second),
		MaxReapInterval:     parseDurationOpt(cfg.MaxReapInterval, 30*time.Second),
		DispatchTimeout:     parseDurationOpt(cfg.DispatchTimeout, 5*time.Second),
		WorkerStopTimeout:   parseDurationOpt(cfg.WorkerStopTimeout, 5*time.Second),
		MaxStaleDuration:    parseDurationOpt(cfg.MaxStaleDuration, 1*time.Hour),
		EnableAgentLog:      cfg.EnableAgentLog,
		AgentLogMaxSize:     cfg.AgentLogMaxSize,
		AgentLogMaxAge:      cfg.AgentLogMaxAge,
		AgentLogMaxBackups:  cfg.AgentLogMaxBackups,
		MaxAgentMessages:    cfg.MaxAgentMessages,
	}
}

func NewAgentPool(ctx context.Context, cfg PoolConfig) *AgentPool {
	ctx, cancel := context.WithCancel(ctx)
	p := &AgentPool{
		cfg:            cfg,
		agents:         make(map[string]*AgentInstance),
		results:        newResultQueue(),
		mailboxes:      make(map[string]*Mailbox),
		groups:         make(map[string]chan struct{}),
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
func (p *AgentPool) Add(name string, def *AgentDef, kind AgentKind, agent *Agent, tools *mcp.Registry, io ...transport.UserIO) *AgentInstance {
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

	inst := &AgentInstance{
		Def:       def,
		Kind:      kind,
		agent:     subAgent,
		pool:      p,
		createdAt: time.Now(),
		status:    "idle",
		doneCh:    make(chan struct{}),
	}
	inst.taskCond = sync.NewCond(&inst.taskMu)

	// Use group-level semaphore if agent belongs to a group
	if def.Group != "" && p.cfg.GroupLimits[def.Group] > 0 {
		p.mu.Lock()
		sem, ok := p.groups[def.Group]
		if !ok {
			sem = make(chan struct{}, p.cfg.GroupLimits[def.Group])
			p.groups[def.Group] = sem
		}
		p.mu.Unlock()
		inst.sem = sem
	} else {
		inst.sem = p.sem
	}
	if len(io) > 0 && io[0] != nil {
		inst.io = io[0]
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
	go p.workerLoop(inst)

	zap.S().Infow("agent added to pool",
		"name", name,
		"kind", kind,
		"tools", def.Tools,
		"workspace", def.Workspace,
	)
	return inst
}

// workerLoop processes tasks from the agent's priority queue.
func (p *AgentPool) workerLoop(inst *AgentInstance) {
	defer p.wg.Done()
	defer close(inst.doneCh)
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			zap.S().Errorw("agent panic recovered",
				"name", inst.Def.Name,
				"recover", fmt.Sprintf("%v", r),
				"stack", string(stack),
			)
			inst.mu.Lock()
			inst.status = "error"
			inst.mu.Unlock()
			if !p.results.push(TaskResult{
				AgentName: inst.Def.Name,
				Success:   false,
				Status:    "error",
				Error:     fmt.Sprintf("panic: %v\n%s", r, string(stack)),
			}) {
				zap.S().Warnw("result queue closed or full, dropping panic result",
					"name", inst.Def.Name,
				)
			}
		}
	}()

	for {
		// Check pool context before blocking on popTask
		select {
		case <-p.coordinatorCtx.Done():
			return
		default:
		}

		task, ok := inst.popTask()
		if !ok {
			return
		}
		p.processTask(inst, task)
	}
}

// processTask runs a single task on the agent.
func (p *AgentPool) processTask(inst *AgentInstance, task Task) {
	// Acquire concurrency slot
	select {
	case inst.sem <- struct{}{}:
	case <-p.coordinatorCtx.Done():
		return
	}
	defer func() { <-inst.sem }()

	inst.mu.Lock()
	inst.status = "busy"
	inst.tasksDone++
	inst.currentTaskID = task.ID
	inst.sessionID = p.parentSessionID
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

	events := make([]TaskEvent, 0, 4)
	events = append(events, TaskEvent{
		Type:      TaskProcessing,
		Timestamp: time.Now(),
	})

	// Open agent execution log if enabled
	var agentLog io.WriteCloser
	if p.cfg.EnableAgentLog && inst.Def.Workspace != "" {
		maxSize := p.cfg.AgentLogMaxSize
		if maxSize <= 0 {
			maxSize = 100
		}
		maxAge := p.cfg.AgentLogMaxAge
		if maxAge <= 0 {
			maxAge = 30
		}
		maxBackups := p.cfg.AgentLogMaxBackups
		if maxBackups <= 0 {
			maxBackups = 3
		}
		agentLog = &lumberjack.Logger{
			Filename:   filepath.Join(inst.Def.Workspace, "agent.log"),
			MaxSize:    maxSize,
			MaxAge:     maxAge,
			MaxBackups: maxBackups,
			LocalTime:  true,
			Compress:   true,
		}
		defer agentLog.Close()
		fmt.Fprintf(agentLog, "[%s] [processing] task=%s input=%s\n",
			time.Now().Format(time.RFC3339), task.ID, truncateString(task.Input, 200))
	}

	// Build system prompt for this agent
	inst.agent.ctxBuilder.SetRenderData(ctxpkg.NewRenderData(inst.agent.cfg))
	systemPrompt, err := inst.agent.ctxBuilder.BuildForAgent(inst.Def.Name)
	if err != nil {
		zap.S().Errorw("build agent context failed", "agent", inst.Def.Name, "error", err)
		systemPrompt = "You are a helpful assistant."
	}
	// Prepend agent role so the LLM sees its core instructions
	if inst.Def.Role != "" {
		systemPrompt = inst.Def.Role + "\n\n" + systemPrompt
	}

	zap.S().Debugw("agent processing task",
		"agent", inst.Def.Name,
		"task_id", task.ID,
		"timeout", timeout,
	)

	// Initialize persistent session on first task
	if inst.loopState == nil {
		sess, err := inst.agent.sessMgr.NewSessionWithParent(inst.agent.cfg.Session.MaxLoop, p.parentSessionID)
		if err != nil {
			zap.S().Errorw("create session for agent failed", "agent", inst.Def.Name, "error", err)
			p.results.push(TaskResult{
				AgentName:  inst.Def.Name,
				TaskID:     task.ID,
				Success:    false,
				Status:     "error",
				Error:      fmt.Sprintf("create session: %v", err),
				DurationMs: 0,
				Events:     append(events, TaskEvent{Type: TaskFailed, Timestamp: time.Now()}),
			})
			return
		}
		inst.loopState = &LoopState{Sess: sess}
	}
	inst.loopState.Turn++
	inst.loopState.Sess.Turn = inst.loopState.Turn
	inst.loopState.Messages = append(inst.loopState.Messages,
		provider.Message{Role: "user", Content: provider.TextContent(task.Input)})

	// Create IO for this task
	taskIO := inst.io
	if taskIO == nil {
		taskIO = NewChannelIO(task.Input)
	}

	var taskEnd func()
	if TelemetryCallbacks.OnTaskSpan != nil {
		taskEnd = TelemetryCallbacks.OnTaskSpan(taskCtx, inst.Def.Name, task.ID)
	}
	start := time.Now()
	taskErr := inst.agent.runTurn(taskCtx, inst.loopState, systemPrompt, taskIO, inst.agent.toolReg, toolNames(inst.agent.toolReg))
	if taskEnd != nil {
		taskEnd()
	}

	result := TaskResult{
		AgentName:  inst.Def.Name,
		TaskID:     task.ID,
		DurationMs: time.Since(start).Milliseconds(),
		Events:     events,
	}
	if taskErr != nil {
		zap.S().Debugw("agent task finished with error",
			"agent", inst.Def.Name,
			"task_id", task.ID,
			"error", taskErr,
		)
		result.Success = false
		result.Error = taskErr.Error()
		switch {
		case strings.Contains(result.Error, "cancel"):
			result.Status = "cancelled"
		case strings.Contains(result.Error, "deadline") || strings.Contains(result.Error, "timeout"):
			result.Status = "timeout"
		default:
			result.Status = "error"
		}
	} else {
		result.Output = extractFinalResponse(inst.loopState.Messages)
		result.Success = true
		result.Status = "completed"
	}

	// Cap message history to prevent unbounded growth (P0 memory leak).
	// The compressor is the primary mechanism; this is a safety net for default DropCompressor.
	maxMsgs := p.cfg.MaxAgentMessages
	if maxMsgs > 0 && inst.loopState != nil && len(inst.loopState.Messages) > maxMsgs {
		inst.loopState.Messages = inst.loopState.Messages[len(inst.loopState.Messages)-maxMsgs:]
	}

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

	switch result.Status {
	case "cancelled":
		events = append(events, TaskEvent{Type: TaskCancelled, Timestamp: time.Now()})
	case "completed":
		events = append(events, TaskEvent{Type: TaskCompleted, Timestamp: time.Now()})
	default:
		events = append(events, TaskEvent{Type: TaskFailed, Timestamp: time.Now()})
	}
	result.Events = events

	// Log completion if agent log is active
	if agentLog != nil {
		fmt.Fprintf(agentLog, "[%s] [%s] task=%s success=%v duration_ms=%d\n",
			time.Now().Format(time.RFC3339), result.Status, task.ID, result.Success, result.DurationMs)
		if result.Error != "" {
			fmt.Fprintf(agentLog, "[%s] [error] task=%s error=%s\n",
				time.Now().Format(time.RFC3339), task.ID, result.Error)
		}
	}

	if !p.results.push(result) {
		zap.S().Errorw("result queue full, dropping result", "agent", inst.Def.Name, "task_id", task.ID)
	}
}

// Dispatch sends a task to an agent. Tries non-blocking first; if the task channel
// is full and DispatchTimeout > 0, falls through to a blocking send with timeout.
func (p *AgentPool) Dispatch(agentName string, task Task) error {
	p.mu.RLock()
	inst, ok := p.agents[agentName]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	// Push to priority queue (unbounded, non-blocking for the caller)
	if !inst.pushTask(task, task.Priority) {
		return fmt.Errorf("agent %s task queue closed", agentName)
	}
	p.recordDispatch(agentName)
	return nil
}

// DispatchBlocking sends a task to an agent, blocking until the task is
// enqueued or the context is cancelled.
func (p *AgentPool) DispatchBlocking(ctx context.Context, agentName string, task Task) error {
	p.mu.RLock()
	inst, ok := p.agents[agentName]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	// Use a goroutine to make pushTask cancellable via context
	done := make(chan struct{}, 1)
	go func() {
		inst.pushTask(task, task.Priority)
		done <- struct{}{}
	}()
	select {
	case <-done:
		p.recordDispatch(agentName)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("dispatch to %s cancelled: %w", agentName, ctx.Err())
	}
}

// recordDispatch updates telemetry after a successful dispatch.
func (p *AgentPool) recordDispatch(agentName string) {
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
}

// Collect drains all completed task results (non-blocking).
func (p *AgentPool) Collect() []TaskResult {
	return p.results.popAll()
}

// PollResult looks up a completed result by task ID. Returns nil if not
// yet available. Non-blocking — intended for synchronous wait-and-poll
// patterns like scope router dispatch.
func (p *AgentPool) PollResult(taskID string) *TaskResult {
	return p.results.findByID(taskID)
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

// getMailbox returns (or creates) the mailbox for an agent name.
func (p *AgentPool) getMailbox(name string) *Mailbox {
	p.mu.Lock()
	defer p.mu.Unlock()
	mb, ok := p.mailboxes[name]
	if !ok {
		mb = newMailbox()
		p.mailboxes[name] = mb
	}
	return mb
}

// SendMessage delivers a message to a specific agent's mailbox.
func (p *AgentPool) SendMessage(from, to, subject, body string) error {
	p.mu.RLock()
	_, ok := p.agents[to]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent not found: %s", to)
	}

	mb := p.getMailbox(to)
	mb.mu.Lock()
	mb.messages = append(mb.messages, AgentMessage{
		From:    from,
		To:      to,
		Subject: subject,
		Body:    body,
		SentAt:  time.Now(),
	})
	mb.cond.Signal()
	mb.mu.Unlock()
	return nil
}

// ReadMessages drains all messages from an agent's mailbox (non-blocking).
func (p *AgentPool) ReadMessages(agentName string) []AgentMessage {
	mb := p.getMailbox(agentName)
	mb.mu.Lock()
	msgs := mb.messages
	mb.messages = nil
	mb.mu.Unlock()
	return msgs
}

// BroadcastMessage sends a message to all agents in the pool.
func (p *AgentPool) BroadcastMessage(from, subject, body string) {
	p.mu.RLock()
	names := make([]string, 0, len(p.agents))
	for name := range p.agents {
		names = append(names, name)
	}
	p.mu.RUnlock()

	for _, name := range names {
		mb := p.getMailbox(name)
		mb.mu.Lock()
		mb.messages = append(mb.messages, AgentMessage{
			From:    from,
			To:      name,
			Subject: subject,
			Body:    body,
			SentAt:  time.Now(),
		})
		mb.cond.Signal()
		mb.mu.Unlock()
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
			Name:          name,
			Kind:          inst.Kind.String(),
			Role:          inst.Def.Role,
			Status:        inst.status,
			TasksDone:     inst.tasksDone,
			Workspace:     inst.Def.Workspace,
			SessionID:     string(inst.sessionID),
			CurrentTaskID: inst.currentTaskID,
			Tools:         inst.Def.Tools,
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
func (p *AgentPool) cleanOrphanedWorkspaces() {
	if p.cfg.WorkspaceDir == "" {
		return
	}
	entries, err := os.ReadDir(p.cfg.WorkspaceDir)
	if err != nil {
		return
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "temp-") {
			continue
		}
		agentName := strings.TrimPrefix(entry.Name(), "temp-")
		if _, exists := p.agents[agentName]; !exists {
			orphanPath := filepath.Join(p.cfg.WorkspaceDir, entry.Name())
			if err := os.RemoveAll(orphanPath); err != nil {
				zap.S().Warnw("failed to remove orphaned workspace",
					"path", orphanPath, "error", err)
			} else {
				zap.S().Infow("removed orphaned workspace", "path", orphanPath)
			}
		}
	}
}

func (p *AgentPool) Remove(name string) bool {
	p.mu.Lock()
	inst, ok := p.agents[name]
	if ok {
		delete(p.agents, name)
		delete(p.mailboxes, name)
		agentPoolSize.Add(-1)
		if TelemetryCallbacks.OnPoolSize != nil {
			TelemetryCallbacks.OnPoolSize(int64(len(p.agents)))
		}
	}
	if p.OnRemoveAgent != nil {
		p.OnRemoveAgent(name)
	}
	p.mu.Unlock()
	if ok {
		// Cancel any running task first
		inst.mu.Lock()
		if inst.cancel != nil {
			inst.cancel()
		}
		inst.mu.Unlock()

		inst.closeTasks()

		// Wait for worker to exit (with timeout to prevent deadlock)
		select {
		case <-inst.doneCh:
		case <-time.After(p.cfg.WorkerStopTimeout):
			zap.S().Warnw("timeout waiting for agent worker to exit",
				"name", name,
			)
		}

		p.cleanupSession(inst)
		p.cleanWorkspace(inst)
		return true
	}
	return false
}

// Forget resets the persistent conversation context for an agent.
// Generates a summary of the current session and creates a fresh session on next dispatch.
// Returns an error if the agent is not found or is currently busy.
func (p *AgentPool) Forget(name string) error {
	p.mu.RLock()
	inst, ok := p.agents[name]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("agent not found: %s", name)
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.status == "busy" {
		return fmt.Errorf("agent %s is busy, cannot forget context", name)
	}

	if inst.loopState == nil {
		return nil // no context to forget
	}

	// Generate summary and close the persistent session
	inst.agent.generateSummary(inst.loopState.Sess, inst.loopState)
	inst.loopState.Sess.Close()
	inst.agent.sessMgr.Remove(inst.loopState.Sess.ID)
	inst.loopState = nil

	zap.S().Infow("agent context forgotten", "name", name)
	return nil
}

// Shutdown gracefully stops the pool: cancels all tasks, waits for goroutines, cleans up.
// SetIO sets the SubAgentIO for an existing agent instance.
func (p *AgentPool) SetIO(name string, io transport.UserIO) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	inst, ok := p.agents[name]
	if !ok {
		return false
	}
	inst.io = io
	return true
}

// cleanupSession closes the persistent session for an agent instance,
// generating a summary and removing it from the session manager.
func (p *AgentPool) cleanupSession(inst *AgentInstance) {
	if inst.loopState == nil {
		return
	}
	inst.agent.generateSummary(inst.loopState.Sess, inst.loopState)
	inst.loopState.Sess.Close()
	inst.agent.sessMgr.Remove(inst.loopState.Sess.ID)
	inst.loopState = nil
}

func (p *AgentPool) Shutdown() {
	zap.S().Infow("agent pool shutting down...")
	p.cancel() // Cancel all tasks

	// Close all agent task queues to wake up workers
	p.mu.RLock()
	for _, inst := range p.agents {
		inst.closeTasks()
	}
	p.mu.RUnlock()

	p.wg.Wait() // Wait for all goroutines
	p.results.close()
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
func (p *AgentPool) reapIdleAgents() {
	interval := p.cfg.IdleTimeout / 4
	if p.cfg.MaxReapInterval > 0 && interval > p.cfg.MaxReapInterval {
		interval = p.cfg.MaxReapInterval
	}
	if p.cfg.MinReapInterval > 0 && interval < p.cfg.MinReapInterval {
		interval = p.cfg.MinReapInterval
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
				// Check for stale error agents first
				inst.mu.RLock()
				st := inst.status
				cr := inst.createdAt
				inst.mu.RUnlock()
				if st == "error" && p.cfg.MaxStaleDuration > 0 && time.Since(cr) > p.cfg.MaxStaleDuration {
					zap.S().Infow("reaping stale error agent", "name", name)
					delete(p.agents, name)
					delete(p.mailboxes, name)
					agentPoolSize.Add(-1)
					inst.closeTasks()
					if p.OnRemoveAgent != nil {
						p.OnRemoveAgent(name)
					}
					reap = append(reap, inst)
					continue
				}
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
					delete(p.mailboxes, name)
					agentPoolSize.Add(-1)
					inst.closeTasks()
					if p.OnRemoveAgent != nil {
						p.OnRemoveAgent(name)
					}
					reap = append(reap, inst)
				}
			}
			if len(reap) > 0 && TelemetryCallbacks.OnPoolSize != nil {
				TelemetryCallbacks.OnPoolSize(int64(len(p.agents)))
			}
			p.mu.Unlock()

			// Clean up orphaned temp directories (no matching agent in pool)
			p.cleanOrphanedWorkspaces()

			// Wait for workers to exit and clean up (outside the lock)
			for _, inst := range reap {
				select {
				case <-inst.doneCh:
				case <-time.After(p.cfg.WorkerStopTimeout):
					zap.S().Warnw("timeout waiting for reaped agent worker", "name", inst.Def.Name)
				}
				p.cleanupSession(inst)
				p.cleanWorkspace(inst)
			}
		}
	}
}

// truncateString truncates a string to maxLen, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
