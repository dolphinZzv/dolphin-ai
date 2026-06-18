package tui

import (
	"context"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/common"
	"dolphin/internal/limit"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

func init() {
	transport.Register("tui", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
		modelName, _ := cfg["model"].(string)
		showTools := false
		if v, ok := cfg["show_tools"].(bool); ok {
			showTools = v
		}
		showThinking := false
		if v, ok := cfg["show_thinking"].(bool); ok {
			showThinking = v
		}
		workmode, _ := cfg["workmode"].(string)
		poolSize, _ := cfg["pool_size"].(int)
		toolParallelism, _ := cfg["tool_parallelism"].(int)
		temperature, _ := cfg["temperature"].(float64)
		var tempFor func(string) float64
		if f, ok := cfg["temp_for"].(func(string) float64); ok {
			tempFor = f
		}
		var logger *zap.Logger
		if l, ok := cfg["logger"].(*zap.Logger); ok {
			logger = l
		}
		return NewTUI(modelName, showTools, showThinking, workmode, poolSize, toolParallelism, temperature, tempFor, logger), nil
	})
}

type TUI struct {
	*transport.SessionHolder
	id              string
	program         *tea.Program
	pendingAgentIO  *agentio.AgentIO
	priority        bool
	msgChan         chan string
	permCh          chan string
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	agentName       string
	modelName       string
	username        string
	showTools       bool
	showThinking    bool
	workmode        string
	version         string
	poolSize        int
	toolParallelism int
	temperature     float64
	tempFor         func(string) float64
	limiter         *limit.Limiter
	logger          *zap.Logger
}

func NewTUI(modelName string, showTools, showThinking bool, workmode string, poolSize, toolParallelism int, temperature float64, tempFor func(string) float64, logger *zap.Logger) *TUI {
	ctx, cancel := context.WithCancel(context.Background())
	username := os.Getenv("USER")
	return &TUI{
		SessionHolder:   transport.NewSessionHolder(nil),
		id:              "tui",
		msgChan:         make(chan string, 1),
		ctx:             ctx,
		cancel:          cancel,
		agentName:       "Dolphin",
		modelName:       modelName,
		username:        username,
		version:         common.Version,
		showTools:       showTools,
		showThinking:    showThinking,
		workmode:        workmode,
		poolSize:        poolSize,
		toolParallelism: toolParallelism,
		temperature:     temperature,
		tempFor:         tempFor,
		logger:          logger,
	}
}

func (t *TUI) ID() string               { return t.id }
func (t *TUI) Context() string          { return "" }
func (t *TUI) Tools() []common.ToolDesc { return nil }
func (t *TUI) Flush() error {
	if t.program != nil {
		t.program.Send(flushMsg{})
	}
	return nil
}

func (t *TUI) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        true,
		Streamable:         true,
		NestRead:           true,
		RenderTextMarkdown: "markdown",
	}
}

func (t *TUI) Start(_ context.Context) error {
	// Load persisted preferences, but config values take priority.
	if prefs, err := loadPrefs(); err == nil {
		t.showTools = t.showTools || prefs.ShowTools
		t.showThinking = t.showThinking || prefs.ShowThinking
	}

	m := newModel()
	m.msgChan = t.msgChan
	m.username = t.username
	m.agentName = t.agentName
	m.modelName = t.modelName
	m.showTools = t.showTools
	m.showThinking = t.showThinking
	m.workmode = t.workmode
	m.poolSize = t.poolSize
	m.temperature = t.temperature
	m.tempFor = t.tempFor
	m.version = t.version
	m.toolParallelism = t.toolParallelism

	// Set up callbacks for model → TUI communication.
	m.setPriority = func() {
		t.mu.Lock()
		t.priority = true
		t.mu.Unlock()
	}
	m.savePrefs = func() {
		_ = savePrefs(tuiPrefs{
			ShowTools:    m.showTools,
			ShowThinking: m.showThinking,
		})
	}

	t.program = tea.NewProgram(m, tea.WithContext(t.ctx), tea.WithAltScreen())

	// Start the event loop first — Send() blocks until Run() consumes.
	go func() {
		_, err := t.program.Run()
		if err != nil && t.logger != nil {
			t.logger.Error("tui program exited with error", zap.Error(err))
		}
	}()

	// Now that Run() is consuming, it's safe to send deferred messages.
	t.mu.Lock()
	if t.pendingAgentIO != nil {
		t.program.Send(setAgentIOMsg{a: t.pendingAgentIO})
		t.pendingAgentIO = nil
	}
	t.mu.Unlock()

	return nil
}

func (t *TUI) Read(ctx context.Context) (string, error) {
	select {
	case line := <-t.msgChan:
		t.syncSession()
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.ctx.Done():
		return "", t.ctx.Err()
	}
}

func (t *TUI) SetAgentIO(a *agentio.AgentIO) {
	t.mu.Lock()
	t.pendingAgentIO = a
	t.mu.Unlock()
	if t.program != nil {
		t.program.Send(setAgentIOMsg{a: a})
	}
}

func (t *TUI) IsPriority() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.priority
}

func (t *TUI) ResetPriority() {
	t.mu.Lock()
	t.priority = false
	t.mu.Unlock()
}

func (t *TUI) SetLimiter(l *limit.Limiter) {
	t.limiter = l
}

func (t *TUI) syncSession() {
	s := t.Session()
	if s == nil || t.program == nil {
		return
	}
	t.program.Send(sessionMsg{id: s.ID})
	input, _ := s.Get("input_tokens").(int)
	output, _ := s.Get("output_tokens").(int)
	rounds, _ := s.Get("rounds").(int)

	toolCalls, _ := s.Get("tool_calls").(int)
	msg := usageMsg{inputTokens: input, outputTokens: output, rounds: rounds, toolCalls: toolCalls}

	if t.limiter != nil {
		cfg := t.limiter.Config()
		store := t.limiter.Store()
		msg.hardReqs = limit.ReadHardLimit(cfg, "llm.limit.max_requests")
		msg.hardTokens = limit.ReadHardLimit(cfg, "llm.limit.max_total_tokens")
		msg.reqs, _ = store.Get("llm.requests")
		msg.tokens, _ = store.Get("llm.total_tokens")
	}

	t.program.Send(msg)
}

var _ transport.IO = (*TUI)(nil)

func (t *TUI) Write(_ context.Context, text string) error {
	if t.program != nil {
		t.program.Send(contentMsg{text: text})
	}
	return nil
}

func (t *TUI) WriteThinking(_ context.Context, text string) error {
	if t.program != nil {
		t.program.Send(thinkingMsg{text: text})
	}
	return nil
}

func (t *TUI) WriteToolCall(_ context.Context, call types.ToolCall) error {
	if t.program != nil {
		t.program.Send(toolCallMsg{call: call})
	}
	return nil
}

func (t *TUI) WriteToolResult(_ context.Context, result types.ToolResult) error {
	if t.program != nil {
		t.program.Send(toolResultMsg{result: result})
	}
	return nil
}

func (t *TUI) NotifyModelChange(name string) {
	if t.program != nil {
		t.program.Send(modelChangeMsg{name: name})
	}
}

func (t *TUI) NotifySessionID(id string) {
	if t.program != nil {
		t.program.Send(sessionMsg{id: id})
	}
}

func (t *TUI) Close() error {
	t.cancel()
	if t.program != nil {
		t.program.Send(tea.Quit())
	}
	return nil
}

func (t *TUI) RequestPermission(ctx context.Context, prompt string) (transport.PermissionResult, error) {
	ch := make(chan string, 1)
	t.mu.Lock()
	t.permCh = ch
	t.mu.Unlock()

	if t.program != nil {
		t.program.Send(permRequestMsg{prompt: prompt, ch: ch})
	}

	var reply string
	select {
	case reply = <-ch:
	case <-t.ctx.Done():
		t.mu.Lock()
		t.permCh = nil
		t.mu.Unlock()
		return transport.PermissionDenied, t.ctx.Err()
	case <-ctx.Done():
		t.mu.Lock()
		t.permCh = nil
		t.mu.Unlock()
		return transport.PermissionDenied, ctx.Err()
	}

	t.mu.Lock()
	t.permCh = nil
	t.mu.Unlock()

	switch reply {
	case "once":
		return transport.PermissionOnce, nil
	case "always":
		return transport.PermissionAlways, nil
	default:
		return transport.PermissionDenied, nil
	}
}
