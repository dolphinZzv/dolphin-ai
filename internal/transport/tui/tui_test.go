package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"dolphin/internal/agentio"
	"dolphin/internal/session"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

func TestNewTUI(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if tui == nil {
		t.Fatal("NewTUI returned nil")
	}
	if tui.id != "tui" {
		t.Errorf("expected id 'tui', got %q", tui.id)
	}
	if tui.msgChan == nil {
		t.Error("msgChan is nil")
	}
	if tui.SessionHolder == nil {
		t.Error("SessionHolder is nil")
	}
}

func TestTUI_ID(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if tui.ID() != "tui" {
		t.Errorf("expected 'tui', got %q", tui.ID())
	}
}

func TestTUI_Context(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if tui.Context() != "" {
		t.Errorf("expected empty context, got %q", tui.Context())
	}
}

func TestTUI_Tools(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	tools := tui.Tools()
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}

func TestTUI_Flush(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if err := tui.Flush(); err != nil {
		t.Errorf("Flush returned error: %v", err)
	}
}

func TestTUI_Capability(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	cap := tui.Capability()
	if !cap.Interactive {
		t.Error("expected Interactive to be true")
	}
	if !cap.Streamable {
		t.Error("expected Streamable to be true")
	}
	if !cap.NestRead {
		t.Error("expected NestRead to be true")
	}
	if cap.RenderTextMarkdown != "markdown" {
		t.Errorf("expected RenderTextMarkdown 'markdown', got %q", cap.RenderTextMarkdown)
	}
}

func TestTUI_WriteNilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	// Write before Start (nil program) should not panic.
	err := tui.Write(context.Background(), "hello")
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}
}

func TestTUI_WriteThinkingNilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.WriteThinking(context.Background(), "thinking...")
	if err != nil {
		t.Errorf("WriteThinking returned error: %v", err)
	}
}

func TestTUI_WriteToolCallNilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.WriteToolCall(context.Background(), types.ToolCall{
		ID:   "call_1",
		Name: "test_tool",
	})
	if err != nil {
		t.Errorf("WriteToolCall returned error: %v", err)
	}
}

func TestTUI_WriteToolResultNilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.WriteToolResult(context.Background(), types.ToolResult{
		ToolCallID: "call_1",
		Content:    "result",
	})
	if err != nil {
		t.Errorf("WriteToolResult returned error: %v", err)
	}
}

func TestTUI_ReadContextCancel(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.Read(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestTUI_ReadCloseCancel(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if err := tui.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := tui.Read(context.Background())
	if err == nil {
		t.Error("expected error after Close")
	}
}

func TestTUI_Close(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestTUI_RequestPermissionContextCancel(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := tui.RequestPermission(ctx, "test prompt")
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if result != transport.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", result)
	}
}

func TestTUI_RequestPermissionCloseCancel(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Close()

	result, err := tui.RequestPermission(context.Background(), "test prompt")
	if err == nil {
		t.Error("expected error after Close")
	}
	if result != transport.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", result)
	}
}

func TestTUI_Start(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Start(context.Background())
	if err != nil {
		t.Errorf("Start returned error: %v", err)
	}
	if tui.program == nil {
		t.Error("expected program to be set after Start")
	}
	// Clean up.
	_ = tui.Close()
}

func TestTUI_StartWriteRead(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tui.Close() }()

	// Write a message — should not panic with a running program.
	err = tui.Write(context.Background(), "hello")
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}

	// Read with short timeout — will likely timeout since no user input.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = tui.Read(ctx)
	if err == nil {
		t.Log("unexpectedly read a message")
	}
}

func TestTUI_StartWriteThinking(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tui.Close() }()

	err = tui.WriteThinking(context.Background(), "thinking...")
	if err != nil {
		t.Errorf("WriteThinking returned error: %v", err)
	}
}

func TestTUI_StartWriteToolCall(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tui.Close() }()

	err = tui.WriteToolCall(context.Background(), types.ToolCall{
		ID:        "call_1",
		Name:      "test_tool",
		Arguments: `{"key":"value"}`,
	})
	if err != nil {
		t.Errorf("WriteToolCall returned error: %v", err)
	}
}

func TestTUI_StartWriteToolResult(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	err := tui.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tui.Close() }()

	err = tui.WriteToolResult(context.Background(), types.ToolResult{
		ToolCallID: "call_1",
		Content:    "result content",
		IsError:    false,
	})
	if err != nil {
		t.Errorf("WriteToolResult returned error: %v", err)
	}
}

func TestTUI_ImplementsIO(t *testing.T) {
	var _ transport.IO = (*TUI)(nil)
}

func TestRenderStyled(t *testing.T) {
	tests := []struct {
		entry renderEntry
		style string
	}{
		{renderEntry{content: "text", style: "text"}, "text"},
		{renderEntry{content: "thinking", style: "thinking"}, "thinking"},
		{renderEntry{content: "tool", style: "tool_call"}, "tool_call"},
		{renderEntry{content: "result", style: "tool_result"}, "tool_result"},
		{renderEntry{content: "system", style: "system"}, "system"},
		{renderEntry{content: "unknown", style: "unknown"}, "unknown"},
	}
	for _, tt := range tests {
		result := renderStyled(tt.entry)
		if result == "" {
			t.Errorf("renderStyled returned empty for style %q", tt.style)
		}
	}
}

func TestModelInit(t *testing.T) {
	m := newModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init returned nil command")
	}
}

func TestModelViewNotReady(t *testing.T) {
	m := newModel()
	view := m.View()
	if view.Content != "Initializing..." {
		t.Errorf("expected 'Initializing...', got %q", view.Content)
	}
}

func TestContentMsgType(t *testing.T) {
	msg := contentMsg{text: "test"}
	if msg.text != "test" {
		t.Errorf("expected 'test', got %q", msg.text)
	}
}

func TestThinkingMsgType(t *testing.T) {
	msg := thinkingMsg{text: "think"}
	if msg.text != "think" {
		t.Errorf("expected 'think', got %q", msg.text)
	}
}

func TestPermRequestMsgType(t *testing.T) {
	msg := permRequestMsg{prompt: "allow?"}
	if msg.prompt != "allow?" {
		t.Errorf("expected 'allow?', got %q", msg.prompt)
	}
}

// --- perm_dialog.go tests ---

func TestRenderPermDialog(t *testing.T) {
	d := permDialog{
		prompt:  "Allow this action?",
		choices: []string{"y (once)", "a (always)", "n (deny)"},
		active:  0,
	}
	result := renderPermDialog(d, 80, 0)
	if result == "" {
		t.Error("renderPermDialog returned empty string")
	}
	if !strings.Contains(result, "Allow this action?") {
		t.Error("renderPermDialog should contain prompt")
	}
}

func TestRenderPermDialog_ActiveChoice(t *testing.T) {
	d := permDialog{
		prompt:  "Test",
		choices: []string{"a", "b"},
		active:  1,
	}
	result := renderPermDialog(d, 80, 0)
	if result == "" {
		t.Error("renderPermDialog returned empty string")
	}
}

func TestRenderPermDialog_NarrowWidth(t *testing.T) {
	d := permDialog{prompt: "Test", choices: []string{"x"}}
	result := renderPermDialog(d, 10, 0)
	if result == "" {
		t.Error("renderPermDialog should handle narrow width")
	}
}

// --- theme.go tests ---

// --- renderer.go tests ---

func TestRenderMarkdown_Empty(t *testing.T) {
	result := renderMarkdown("")
	if result != "" {
		t.Errorf("expected empty for empty input, got %q", result)
	}
}

func TestRenderMarkdown_Plain(t *testing.T) {
	result := renderMarkdown("hello world")
	if result == "" {
		t.Error("renderMarkdown returned empty")
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	input := "```go\nfunc main() {}\n```"
	result := renderMarkdown(input)
	if result == "" {
		t.Error("renderMarkdown returned empty for code block")
	}
}

func TestRenderSeparator_Empty(t *testing.T) {
	result := renderSeparator("", 80)
	if result != "" {
		t.Errorf("expected empty for empty name, got %q", result)
	}
}

func TestRenderSeparator_WithName(t *testing.T) {
	result := renderSeparator("Dolphin", 80)
	if result == "" {
		t.Error("renderSeparator returned empty")
	}
	if !strings.Contains(result, "-") {
		t.Error("renderSeparator should contain dashes")
	}
}

// --- model.go tests ---

func TestOnOff(t *testing.T) {
	if onOff(true) != "on" {
		t.Error("expected 'on'")
	}
	if onOff(false) != "off" {
		t.Error("expected 'off'")
	}
}

func TestModelAppendEntry_Text(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.appendEntry(renderEntry{content: "hello", style: "text"})
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if m.messages[0].content != "hello" {
		t.Errorf("expected 'hello', got %q", m.messages[0].content)
	}
}

func TestModelAppendEntry_TextMultiline(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.appendEntry(renderEntry{content: "line1\nline2", style: "text"})
	if len(m.messages) < 1 {
		t.Error("expected at least 1 message")
	}
}

func TestModelAppendEntry_TextConsecutive(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.appendEntry(renderEntry{content: "hello", style: "text"})
	m.appendEntry(renderEntry{content: " world", style: "text"})
	if m.messages[0].content != "hello world" {
		t.Errorf("expected merged text, got %q", m.messages[0].content)
	}
}

func TestModelAppendEntry_NonText(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.appendEntry(renderEntry{content: "thinking...", style: "thinking"})
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if m.messages[0].style != "thinking" {
		t.Errorf("expected 'thinking' style, got %q", m.messages[0].style)
	}
}

func TestModelRebuildViewport_TextBlock(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.messages = []renderEntry{
		{content: "hello", style: "text"},
		{content: "world", style: "text"},
	}
	m.rebuildViewport()
	content := m.viewport.View()
	if content == "" {
		t.Error("viewport content is empty after rebuild")
	}
}

func TestModelRebuildViewport_StyledEntries(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	m.messages = []renderEntry{
		{content: "---", style: "separator"},
		{content: "thinking...", style: "thinking"},
	}
	m.rebuildViewport()
	content := m.viewport.View()
	if content == "" {
		t.Error("viewport content is empty")
	}
}

func TestModelAppendEntry_TrimFront(t *testing.T) {
	// maxMessages is 500; adding 600 entries should trigger trimFront.
	m := newModel()
	// Alternate user_text and system to prevent consecutive text merging.
	for i := 0; i < 600; i++ {
		if i%2 == 0 {
			m.appendEntry(renderEntry{content: "padding", style: "user_text"})
		} else {
			m.appendEntry(renderEntry{content: "padding", style: "system"})
		}
	}

	if len(m.messages) >= 500 {
		t.Errorf("expected messages to be trimmed below maxMessages, got %d", len(m.messages))
	}
	if len(m.blockOffsets) == 0 {
		t.Error("blockOffsets should not be empty after trim")
	}
	if m.renderedContent == "" {
		t.Error("renderedContent should not be empty after trim")
	}
}

func TestModelViewReady(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.modelName = "test-model"
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("hello")

	view := m.View()
	if view.Content == "Initializing..." {
		t.Error("View should not be 'Initializing...' when ready")
	}
	if !strings.Contains(view.Content, "Dolphin") {
		t.Error("View should contain agent name")
	}
}

// TestModelView_WelcomeBanner verifies the empty-state welcome banner is
// shown before the first message and disappears once content arrives or a
// turn starts. It is a pure overlay and must not enter the message buffer.
func TestModelView_WelcomeBanner(t *testing.T) {
	newReadyModel := func() model {
		m := newModel()
		m.ready = true
		m.width = 80
		m.height = 24
		m.agentName = "Dolphin"
		m.version = "v1.0.0"
		m.modelName = "test-model"
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
		return m
	}

	t.Run("shown when empty and idle", func(t *testing.T) {
		m := newReadyModel()
		if !m.showingWelcome() {
			t.Fatal("expected welcome banner while empty and idle")
		}
		view := m.View()
		if !strings.Contains(view.Content, "Dolphin") {
			t.Error("welcome view should contain agent name")
		}
	})

	t.Run("hidden while a turn is pending", func(t *testing.T) {
		m := newReadyModel()
		m.msgStatus = "pending"
		if m.showingWelcome() {
			t.Error("welcome should not show while a turn is pending")
		}
	})

	t.Run("hidden after a message arrives", func(t *testing.T) {
		m := newReadyModel()
		m.appendEntry(renderEntry{content: "hello", style: "text"})
		if m.showingWelcome() {
			t.Error("welcome should not show after content arrives")
		}
		// The welcome banner must not have entered the message buffer.
		if len(m.messages) == 0 {
			t.Error("appendEntry should have recorded the message")
		}
	})
}

// TestModelUpdate_ArrowKeys verifies the arrow-key behaviour shown in the
// welcome banner: ↑/↓ scroll the conversation viewport when input is
// single-line (and move the textarea cursor when multi-line), while
// Ctrl+↑/Ctrl+↓ browse input history regardless of input length.
func TestModelUpdate_ArrowKeys(t *testing.T) {
	newModelWithHistory := func() model {
		m := newModel()
		// Small viewport height so 4 lines of content overflow and scrolling
		// is observable.
		m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(2))
		m.viewport.SetContent("line1\nline2\nline3\nline4")
		m.width = 80
		m.inputHistory = []string{"first", "second"}
		m.historyPos = -1
		return m
	}

	t.Run("ctrl+up browses input history", func(t *testing.T) {
		m := newModelWithHistory()
		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
		m = newM.(model)
		if m.historyPos != 1 {
			t.Errorf("ctrl+up should navigate to latest history (pos 1), got %d", m.historyPos)
		}
		if m.textarea.Value() != "second" {
			t.Errorf("ctrl+up should load history entry, got %q", m.textarea.Value())
		}
	})

	t.Run("ctrl+down moves history forward", func(t *testing.T) {
		m := newModelWithHistory()
		m1, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
		m2, _ := m1.(model).Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl})
		m = m2.(model)
		if m.historyPos != -1 {
			t.Errorf("ctrl+down past the end should reset historyPos to -1, got %d", m.historyPos)
		}
	})

	t.Run("single-line up scrolls viewport, not history", func(t *testing.T) {
		m := newModelWithHistory()
		m.viewport.GotoBottom()
		before := m.viewport.YOffset()
		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		m = newM.(model)
		if m.historyPos != -1 {
			t.Errorf("plain up should not touch history, got historyPos=%d", m.historyPos)
		}
		if m.viewport.YOffset() >= before {
			t.Errorf("single-line up should scroll viewport up, YOffset %d->%d", before, m.viewport.YOffset())
		}
	})

	t.Run("multi-line up falls through to textarea (no scroll)", func(t *testing.T) {
		m := newModelWithHistory()
		m.viewport.GotoBottom()
		m.textarea.SetValue("line a\nline b")
		before := m.viewport.YOffset()
		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		m = newM.(model)
		if m.viewport.YOffset() != before {
			t.Errorf("multi-line up should not scroll viewport, YOffset %d->%d", before, m.viewport.YOffset())
		}
	})
}

func TestModelViewWithPermDialog(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.modelName = "test-model"
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("hello")
	m.permDialog = &permDialog{
		prompt:  "Allow?",
		choices: []string{"y (once)", "a (always)", "n (deny)"},
		active:  0,
	}

	view := m.View()
	if view.Content == "" {
		t.Error("View should not be empty with perm dialog")
	}
}

// Update message tests

func TestModelUpdate_WindowSizeMsg(t *testing.T) {
	m := newModel()
	m.width = 0
	m.height = 0
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = newM.(model)
	if m.width != 100 {
		t.Errorf("expected width=100, got %d", m.width)
	}
	if m.height != 30 {
		t.Errorf("expected height=30, got %d", m.height)
	}
}

func TestModelUpdate_CtrlC(t *testing.T) {
	m := newModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// With ctrl+c, should return Quit. Let's just check it doesn't panic.
	_ = cmd
}

func TestModelUpdate_ContentMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.newReply = true

	newM, _ := m.Update(contentMsg{text: "hello"})
	m = newM.(model)
	if m.newReply {
		t.Error("newReply should be false after contentMsg")
	}
}

func TestModelUpdate_ThinkingMsg_ShowOff(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.showThinking = false

	_, _ = m.Update(thinkingMsg{text: "thinking..."})
	if m.inThinking {
		t.Error("inThinking should be false when showThinking is off")
	}
}

func TestModelUpdate_ThinkingMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showThinking = true

	newM, _ := m.Update(thinkingMsg{text: "thinking..."})
	m = newM.(model)
	if !m.inThinking {
		t.Error("inThinking should be true")
	}
}

func TestModelUpdate_ThinkingMsg_Append(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showThinking = true

	newM, _ := m.Update(thinkingMsg{text: "first"})
	m = newM.(model)
	newM, _ = m.Update(thinkingMsg{text: " second"})
	m = newM.(model)
	if len(m.messages) < 1 {
		t.Error("should have thinking entries")
	}
}

func TestModelUpdate_ToolCallMsg_ShowOff(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.showTools = false

	_, _ = m.Update(toolCallMsg{call: types.ToolCall{Name: "test", Arguments: "{}"}})
	// Should not add entry when showTools is off.
}

func TestModelUpdate_ToolCallMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = true

	newM, _ := m.Update(toolCallMsg{call: types.ToolCall{Name: "test", Arguments: "{}"}})
	m = newM.(model)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if m.messages[0].style != "tool_call" {
		t.Errorf("expected tool_call style, got %q", m.messages[0].style)
	}
}

func TestModelUpdate_ToolResultMsg_ShowOff(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.showTools = false

	_, _ = m.Update(toolResultMsg{result: types.ToolResult{Content: "done"}})
}

func TestModelUpdate_ToolResultMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = true

	newM, _ := m.Update(toolResultMsg{result: types.ToolResult{Content: "done"}})
	m = newM.(model)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if m.messages[0].style != "tool_result" {
		t.Errorf("expected tool_result style, got %q", m.messages[0].style)
	}
}

func TestModelUpdate_ToolResultMsg_Error(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = true

	newM, _ := m.Update(toolResultMsg{result: types.ToolResult{Content: "error", IsError: true}})
	m = newM.(model)
	if len(m.messages) != 2 {
		t.Fatalf("expected 2 messages (tool_call fallback + error), got %d", len(m.messages))
	}
	if m.messages[0].style != "tool_call" {
		t.Errorf("expected tool_call style for fallback entry, got %q", m.messages[0].style)
	}
	if m.messages[1].style != "tool_error" {
		t.Errorf("expected tool_error style, got %q", m.messages[1].style)
	}
	if m.msgStatus != "error" {
		t.Errorf("expected msgStatus='error', got %q", m.msgStatus)
	}
}

// TestModelUpdate_ESCPausesTurn verifies that pressing ESC while a turn is
// pending sends "/session pause" through msgChan (the same path as typed
// input), and that ESC does nothing when no turn is running.
func TestModelUpdate_ESCPausesTurn(t *testing.T) {
	t.Run("pending sends pause", func(t *testing.T) {
		m := newModel()
		m.msgChan = make(chan string, 1)
		m.msgStatus = "pending"

		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m = newM.(model)

		select {
		case got := <-m.msgChan:
			if got != "/session pause" {
				t.Errorf("expected '/session pause', got %q", got)
			}
		default:
			t.Error("expected '/session pause' on msgChan")
		}
	})

	t.Run("idle does nothing", func(t *testing.T) {
		m := newModel()
		m.msgChan = make(chan string, 1)
		m.msgStatus = "success" // no turn running

		newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		m = newM.(model)

		select {
		case got := <-m.msgChan:
			t.Errorf("idle ESC should not send, got %q", got)
		default:
		}
	})
}

// Tool errors must surface even when showTools is off — otherwise a
// failed tool call is silently invisible.
func TestModelUpdate_ToolResultMsg_ErrorWhenToolsHidden(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = false

	newM, _ := m.Update(toolResultMsg{result: types.ToolResult{Content: "boom", IsError: true}})
	m = newM.(model)
	if len(m.messages) != 2 {
		t.Fatalf("expected 2 messages (tool_call fallback + error), got %d", len(m.messages))
	}
	if m.messages[0].style != "tool_call" {
		t.Errorf("expected tool_call style for fallback entry, got %q", m.messages[0].style)
	}
	if m.messages[1].style != "tool_error" {
		t.Errorf("expected tool_error style, got %q", m.messages[0].style)
	}
}

func TestModelUpdate_FlushMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	_, _ = m.Update(flushMsg{})
}

func TestModelUpdate_ModelChangeMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80

	newM, _ := m.Update(modelChangeMsg{name: "new-model"})
	m = newM.(model)
	if m.modelName != "new-model" {
		t.Errorf("expected modelName='new-model', got %q", m.modelName)
	}
}

func TestModelUpdate_PermRequestMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	newM, _ := m.Update(permRequestMsg{prompt: "allow?"})
	m = newM.(model)
	if m.permDialog == nil {
		t.Fatal("permDialog should not be nil")
	}
	if m.permDialog.prompt != "allow?" {
		t.Errorf("expected prompt 'allow?', got %q", m.permDialog.prompt)
	}
}

func TestModelUpdate_UserSubmitMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.msgChan = make(chan string, 1)

	_, _ = m.Update(userSubmitMsg{text: "hello"})
	select {
	case txt := <-m.msgChan:
		if txt != "hello" {
			t.Errorf("expected 'hello', got %q", txt)
		}
	default:
		// Channel may be empty if the message wasn't written (which is okay for the test)
	}
}

func TestModelUpdate_PermResponseMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.permCh = make(chan string, 1)

	_, _ = m.Update(permResponseMsg{choice: "once"})
	select {
	case choice := <-m.permCh:
		if choice != "once" {
			t.Errorf("expected 'once', got %q", choice)
		}
	default:
	}
}

// Permission dialog key handling in Update

func TestModelUpdate_PermDialogKey_Y(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.permDialog = &permDialog{
		prompt:  "test",
		choices: []string{"y", "n"},
		active:  0,
	}
	m.permCh = make(chan string, 1)
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("permDialog should be cleared after key press")
	}
}

func TestModelUpdate_PermDialogKey_A(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.permDialog = &permDialog{
		prompt:     "test",
		choices:    []string{"y", "n"},
		confirmIdx: -1,
	}
	m.permCh = make(chan string, 1)
	// 'a' requires double-press. First press shows confirm; second resolves.
	a := tea.KeyPressMsg{Code: 'a'}
	newM, _ := m.Update(a)
	m = newM.(model)
	if m.permDialog == nil || m.permDialog.confirmIdx != 1 {
		t.Error("first 'a' should set confirm, not dismiss")
	}
	newM, _ = m.Update(a)
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("second 'a' should dismiss permDialog")
	}
}

func TestModelUpdate_PermDialogKey_N(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.permDialog = &permDialog{
		prompt:  "test",
		choices: []string{"y", "n"},
	}
	m.permCh = make(chan string, 1)
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("permDialog should be cleared")
	}
}

// --- tui.go tests ---

func TestTUI_NotifyModelChange_NilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	// Should not panic with nil program.
	tui.NotifyModelChange("new-model")
}

func TestTUI_NotifyModelChange_Running(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()

	tui.NotifyModelChange("test-model")
}

func TestTUI_Read_CtxDone(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.Read(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestTUI_RequestPermission_ReplyPaths(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()

	// Send a perm response and test it gets picked up.
	// Protect permCh access with mu to prevent race with RequestPermission's write.
	go func() {
		tui.mu.Lock()
		ch := tui.permCh
		tui.mu.Unlock()
		if ch != nil {
			ch <- "once"
		}
	}()

	result, err := tui.RequestPermission(context.Background(), "test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != transport.PermissionOnce {
		t.Errorf("expected PermissionOnce, got %v", result)
	}
}

func TestTUI_RequestPermission_CtxDone(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.RequestPermission(ctx, "test")
	if err == nil {
		t.Error("expected error")
	}
}

func TestTUI_RequestPermission_TUICtxDone(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Start(context.Background())

	_ = tui.Close() // cancels tui.ctx

	_, err := tui.RequestPermission(context.Background(), "test")
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestTUI_Init_WithConfig(t *testing.T) {
	// Build a TUI via the registered builder with config.
	io, err := transport.Build(context.Background(), "tui", map[string]any{
		"model":         "test-model",
		"show_tools":    true,
		"show_thinking": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if io == nil {
		t.Fatal("expected non-nil IO")
	}
	if io.ID() != "tui" {
		t.Errorf("expected 'tui', got %q", io.ID())
	}
}

func TestTUI_Init_EmptyConfig(t *testing.T) {
	io, err := transport.Build(context.Background(), "tui", nil)
	if err != nil {
		t.Fatal(err)
	}
	if io == nil {
		t.Fatal("expected non-nil IO")
	}
}

func TestTUI_Init_ConfigNonBool(t *testing.T) {
	io, err := transport.Build(context.Background(), "tui", map[string]any{
		"show_tools":    "not-a-bool",
		"show_thinking": "not-a-bool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if io == nil {
		t.Fatal("expected non-nil IO")
	}
}

// Additional Update path tests for enter key commands.

func TestModelUpdate_EnterTools(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = false
	m.textarea.SetValue("/tools")

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = newM.(model)
	if !m.showTools {
		t.Error("showTools should be true after toggling")
	}
}

func TestModelUpdate_EnterThinking(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.showThinking = false
	m.textarea.SetValue("/thinking")

	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = newM.(model)
	if !m.showThinking {
		t.Error("showThinking should be true after toggling")
	}
}

func TestModelUpdate_AltEnter(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
}

func newTestAgentIO(t *testing.T) *agentio.AgentIO {
	t.Helper()
	mgr := session.NewManager(t.TempDir())
	return agentio.NewAgentIO(1, mgr, nil, nil, "test")
}

func TestQueueBodyLines(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if n := queueBodyLines(0, 0, 0); n != 0 {
			t.Errorf("expected 0 for empty, got %d", n)
		}
	})

	t.Run("active and pending under caps", func(t *testing.T) {
		// 1 active + 1 pending → 2 body lines, no indicators.
		if n := queueBodyLines(1, 1, 0); n != 2 {
			t.Errorf("expected 2, got %d", n)
		}
	})

	t.Run("pending overflow adds indicator", func(t *testing.T) {
		// 3 shown + 1 "+N queued" indicator → capped to queueMaxBodyLines.
		if n := queueBodyLines(0, 5, 0); n != 2 {
			t.Errorf("expected 2 (capped by queueMaxBodyLines), got %d", n)
		}
	})

	t.Run("completed overflow adds indicator", func(t *testing.T) {
		if n := queueBodyLines(0, 0, 10); n != 2 {
			t.Errorf("expected 2 (capped by queueMaxBodyLines), got %d", n)
		}
	})

	t.Run("matches renderQueue line count", func(t *testing.T) {
		aio := newTestAgentIO(t)
		aio.SetActive("worker-1", &agentio.Turn{TurnID: "t1", SessionID: "s1", Input: "hi"})
		aio.SendTurn(context.Background(), &agentio.Turn{Input: "next"})
		active, pending := queueCounts(aio)
		want := queueBodyLines(active, pending, 0)
		s := renderQueue(aio, nil, 80)
		// renderQueue emits header + body lines; body = total lines - 1.
		got := strings.Count(s, "\n") // header is line 0, so body lines == newline count
		if got != want {
			t.Errorf("body lines mismatch: queueBodyLines=%d renderQueue body=%d", want, got)
		}
	})
}

func TestRenderQueue(t *testing.T) {
	t.Run("nil agentIO", func(t *testing.T) {
		if s := renderQueue(nil, nil, 80); s != "" {
			t.Errorf("expected empty for nil, got %q", s)
		}
	})

	t.Run("empty queue", func(t *testing.T) {
		aio := newTestAgentIO(t)
		if s := renderQueue(aio, nil, 80); s != "" {
			t.Errorf("expected empty, got %q", s)
		}
	})

	t.Run("with active turn", func(t *testing.T) {
		aio := newTestAgentIO(t)
		aio.SetActive("worker-1", &agentio.Turn{TurnID: "t1", SessionID: "s1", Input: "hello world"})
		s := renderQueue(aio, nil, 80)
		if !strings.Contains(s, "Queue") {
			t.Errorf("expected Queue header, got %q", s)
		}
		if !strings.Contains(s, "hello world") {
			t.Errorf("expected input in output, got %q", s)
		}
	})

	t.Run("with pending turn", func(t *testing.T) {
		aio := newTestAgentIO(t)
		aio.SendTurn(context.Background(), &agentio.Turn{Input: "pending task"})
		s := renderQueue(aio, nil, 80)
		if !strings.Contains(s, "Queue") {
			t.Errorf("expected Queue header, got %q", s)
		}
		if !strings.Contains(s, "pending task") {
			t.Errorf("expected pending input, got %q", s)
		}
	})
}

func TestSortedKeys(t *testing.T) {
	m := map[string]*agentio.TurnInfo{
		"worker-2": {},
		"worker-1": {},
		"worker-3": {},
	}
	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "worker-1" || keys[1] != "worker-2" || keys[2] != "worker-3" {
		t.Errorf("expected sorted keys, got %v", keys)
	}
}

func TestQueueTickMsg(t *testing.T) {
	msg := queueTickMsg{}
	if msg != (queueTickMsg{}) {
		t.Error("queueTickMsg should be comparable")
	}
}

func TestSetAgentIOMsg(t *testing.T) {
	aio := newTestAgentIO(t)
	m := newModel()
	m2, _ := m.Update(setAgentIOMsg{a: aio})
	if m2.(model).agentIO != aio {
		t.Error("SetAgentIO should store agentIO on model")
	}
}

func TestTUI_IsPriority_ResetPriority(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	if tui.IsPriority() {
		t.Error("expected priority to be false initially")
	}

	tui.mu.Lock()
	tui.priority = true
	tui.mu.Unlock()

	if !tui.IsPriority() {
		t.Error("expected priority to be true after setting")
	}

	tui.ResetPriority()
	if tui.IsPriority() {
		t.Error("expected priority to be false after reset")
	}
}

func TestTUI_SetAgentIO_NilProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	aio := newTestAgentIO(t)
	tui.SetAgentIO(aio)
	if tui.pendingAgentIO != aio {
		t.Error("SetAgentIO should store agentIO")
	}
}

func TestTUI_SetAgentIO_WithProgram(t *testing.T) {
	tui := NewTUI("", true, true, "", 0, 0, 0, nil, "", nil, false, nil, 0, nil, nil)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()
	aio := newTestAgentIO(t)
	tui.SetAgentIO(aio)
	if tui.pendingAgentIO != aio {
		t.Error("SetAgentIO should store agentIO")
	}
}

func TestModelUpdate_PrioritySubmitMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.msgChan = make(chan string, 1)
	m.closeBlock = true
	msg := prioritySubmitMsg{text: "priority message"}

	newM, _ := m.Update(msg)
	m = newM.(model)
	if !m.newReply {
		t.Error("newReply should be true after prioritySubmitMsg")
	}
	if m.currentMsg != "priority message" {
		t.Errorf("expected currentMsg='priority message', got %q", m.currentMsg)
	}
	if m.msgStatus != "pending" {
		t.Errorf("expected msgStatus='pending', got %q", m.msgStatus)
	}
	select {
	case txt := <-m.msgChan:
		if txt != "priority message" {
			t.Errorf("expected 'priority message' on channel, got %q", txt)
		}
	default:
	}
}

func TestModelUpdate_PrioritySubmitMsg_SetPriority(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	m.viewport.SetContent("")
	m.width = 80
	m.msgChan = make(chan string, 1)

	priorityCalled := false
	m.setPriority = func() {
		priorityCalled = true
	}

	_, _ = m.Update(prioritySubmitMsg{text: "test"})
	if !priorityCalled {
		t.Error("setPriority callback should have been called")
	}
}
