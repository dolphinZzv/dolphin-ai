package tui

import (
	"context"
	"os"
	"sync"

	"dolphin/internal/common"
	"dolphin/internal/transport"
	"dolphin/internal/types"

	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	transport.Register("tui", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
		themeName := "light"
		if name, ok := cfg["theme"].(string); ok && name != "" {
			themeName = name
		}
		modelName, _ := cfg["model"].(string)
		showTools := false
		if v, ok := cfg["show_tools"].(bool); ok {
			showTools = v
		}
		showThinking := false
		if v, ok := cfg["show_thinking"].(bool); ok {
			showThinking = v
		}
		return NewTUI(themeName, modelName, showTools, showThinking), nil
	})
}

type TUI struct {
	*transport.SessionHolder
	id           string
	program      *tea.Program
	msgChan      chan string
	permCh       chan string
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	agentName    string
	modelName    string
	username     string
	theme        Theme
	themeName    string
	showTools    bool
	showThinking bool
}

func NewTUI(themeName, modelName string, showTools, showThinking bool) *TUI {
	ctx, cancel := context.WithCancel(context.Background())
	username := os.Getenv("USER")
	return &TUI{
		SessionHolder: transport.NewSessionHolder(nil),
		id:            "tui",
		msgChan:       make(chan string, 1),
		ctx:           ctx,
		cancel:        cancel,
		agentName:     "Dolphin",
		modelName:     modelName,
		username:      username,
		theme:         ThemeFromString(themeName),
		themeName:     themeName,
		showTools:     showTools,
		showThinking:  showThinking,
	}
}

func (t *TUI) ID() string               { return t.id }
func (t *TUI) Context() string          { return "" }
func (t *TUI) Tools() []common.ToolDesc { return nil }
func (t *TUI) Flush() error             { return nil }

func (t *TUI) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        true,
		Streamable:         true,
		NestRead:           true,
		RenderTextMarkdown: "markdown",
	}
}

func (t *TUI) Start(_ context.Context) error {
	ApplyTheme(t.theme)
	m := newModel()
	m.msgChan = t.msgChan
	m.permCh = t.permCh
	m.username = t.username
	m.agentName = t.agentName
	m.modelName = t.modelName
	m.theme = t.theme
	m.themeName = t.themeName
	t.program = tea.NewProgram(m, tea.WithContext(t.ctx))

	go func() {
		_, _ = t.program.Run()
	}()

	return nil
}

func (t *TUI) Read(ctx context.Context) (string, error) {
	select {
	case line := <-t.msgChan:
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.ctx.Done():
		return "", t.ctx.Err()
	}
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
		t.program.Send(permRequestMsg{prompt: prompt})
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
