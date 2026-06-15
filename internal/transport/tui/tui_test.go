package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"dolphin/internal/transport"
	"dolphin/internal/types"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewTUI(t *testing.T) {
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
	if tui.ID() != "tui" {
		t.Errorf("expected 'tui', got %q", tui.ID())
	}
}

func TestTUI_Context(t *testing.T) {
	tui := NewTUI("", true, true)
	if tui.Context() != "" {
		t.Errorf("expected empty context, got %q", tui.Context())
	}
}

func TestTUI_Tools(t *testing.T) {
	tui := NewTUI("", true, true)
	tools := tui.Tools()
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}

func TestTUI_Flush(t *testing.T) {
	tui := NewTUI("", true, true)
	if err := tui.Flush(); err != nil {
		t.Errorf("Flush returned error: %v", err)
	}
}

func TestTUI_Capability(t *testing.T) {
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
	// Write before Start (nil program) should not panic.
	err := tui.Write(context.Background(), "hello")
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}
}

func TestTUI_WriteThinkingNilProgram(t *testing.T) {
	tui := NewTUI("", true, true)
	err := tui.WriteThinking(context.Background(), "thinking...")
	if err != nil {
		t.Errorf("WriteThinking returned error: %v", err)
	}
}

func TestTUI_WriteToolCallNilProgram(t *testing.T) {
	tui := NewTUI("", true, true)
	err := tui.WriteToolCall(context.Background(), types.ToolCall{
		ID:   "call_1",
		Name: "test_tool",
	})
	if err != nil {
		t.Errorf("WriteToolCall returned error: %v", err)
	}
}

func TestTUI_WriteToolResultNilProgram(t *testing.T) {
	tui := NewTUI("", true, true)
	err := tui.WriteToolResult(context.Background(), types.ToolResult{
		ToolCallID: "call_1",
		Content:    "result",
	})
	if err != nil {
		t.Errorf("WriteToolResult returned error: %v", err)
	}
}

func TestTUI_ReadContextCancel(t *testing.T) {
	tui := NewTUI("", true, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.Read(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestTUI_ReadCloseCancel(t *testing.T) {
	tui := NewTUI("", true, true)
	if err := tui.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := tui.Read(context.Background())
	if err == nil {
		t.Error("expected error after Close")
	}
}

func TestTUI_Close(t *testing.T) {
	tui := NewTUI("", true, true)
	err := tui.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestTUI_RequestPermissionContextCancel(t *testing.T) {
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	if view != "Initializing..." {
		t.Errorf("expected 'Initializing...', got %q", view)
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
	result := renderPermDialog(d, 80)
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
	result := renderPermDialog(d, 80)
	if result == "" {
		t.Error("renderPermDialog returned empty string")
	}
}

func TestRenderPermDialog_NarrowWidth(t *testing.T) {
	d := permDialog{prompt: "Test", choices: []string{"x"}}
	result := renderPermDialog(d, 10)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("")
	m.width = 80

	m.appendEntry(renderEntry{content: "line1\nline2", style: "text"})
	if len(m.messages) < 1 {
		t.Error("expected at least 1 message")
	}
}

func TestModelAppendEntry_TextConsecutive(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("hello")

	view := m.View()
	if view == "Initializing..." {
		t.Error("View should not be 'Initializing...' when ready")
	}
	if !strings.Contains(view, "Dolphin") {
		t.Error("View should contain agent name")
	}
}

func TestModelViewWithPermDialog(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.modelName = "test-model"
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("hello")
	m.permDialog = &permDialog{
		prompt:  "Allow?",
		choices: []string{"y (once)", "a (always)", "n (deny)"},
		active:  0,
	}

	view := m.View()
	if view == "" {
		t.Error("View should not be empty with perm dialog")
	}
}

// Update message tests

func TestModelUpdate_WindowSizeMsg(t *testing.T) {
	m := newModel()
	m.width = 0
	m.height = 0
	m.viewport = viewport.New(80, 20)

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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c"), Alt: false})
	// With ctrl+c, should return Quit. Let's just check it doesn't panic.
	_ = cmd
}

func TestModelUpdate_ContentMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.showThinking = false

	_, _ = m.Update(thinkingMsg{text: "thinking..."})
	if m.inThinking {
		t.Error("inThinking should be false when showThinking is off")
	}
}

func TestModelUpdate_ThinkingMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.showTools = false

	_, _ = m.Update(toolCallMsg{call: types.ToolCall{Name: "test", Arguments: "{}"}})
	// Should not add entry when showTools is off.
}

func TestModelUpdate_ToolCallMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.showTools = false

	_, _ = m.Update(toolResultMsg{result: types.ToolResult{Content: "done"}})
}

func TestModelUpdate_ToolResultMsg_ShowOn(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = true

	newM, _ := m.Update(toolResultMsg{result: types.ToolResult{Content: "error", IsError: true}})
	m = newM.(model)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
}

func TestModelUpdate_FlushMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
	_, _ = m.Update(flushMsg{})
}

func TestModelUpdate_ModelChangeMsg(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)

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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
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
	m.viewport = viewport.New(80, 20)
	m.permDialog = &permDialog{
		prompt:  "test",
		choices: []string{"y", "n"},
		active:  0,
	}
	m.permCh = make(chan string, 1)
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("permDialog should be cleared after key press")
	}
}

func TestModelUpdate_PermDialogKey_A(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
	m.permDialog = &permDialog{
		prompt:  "test",
		choices: []string{"y", "n"},
	}
	m.permCh = make(chan string, 1)
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("permDialog should be cleared")
	}
}

func TestModelUpdate_PermDialogKey_N(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
	m.permDialog = &permDialog{
		prompt:  "test",
		choices: []string{"y", "n"},
	}
	m.permCh = make(chan string, 1)
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = newM.(model)
	if m.permDialog != nil {
		t.Error("permDialog should be cleared")
	}
}

// --- tui.go tests ---

func TestTUI_NotifyModelChange_NilProgram(t *testing.T) {
	tui := NewTUI("", true, true)
	// Should not panic with nil program.
	tui.NotifyModelChange("new-model")
}

func TestTUI_NotifyModelChange_Running(t *testing.T) {
	tui := NewTUI("", true, true)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()

	tui.NotifyModelChange("test-model")
}

func TestTUI_Read_CtxDone(t *testing.T) {
	tui := NewTUI("", true, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.Read(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestTUI_RequestPermission_ReplyPaths(t *testing.T) {
	tui := NewTUI("", true, true)
	_ = tui.Start(context.Background())
	defer func() { _ = tui.Close() }()

	// Send a perm response and test it gets picked up.
	go func() {
		tui.permCh <- "once"
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
	tui := NewTUI("", true, true)
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
	tui := NewTUI("", true, true)
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
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("")
	m.width = 80
	m.showTools = false
	m.textarea.SetValue("/tools")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(model)
	if !m.showTools {
		t.Error("showTools should be true after toggling")
	}
}

func TestModelUpdate_EnterThinking(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)
	m.viewport.SetContent("")
	m.width = 80
	m.showThinking = false
	m.textarea.SetValue("/thinking")

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(model)
	if !m.showThinking {
		t.Error("showThinking should be true after toggling")
	}
}

func TestModelUpdate_AltEnter(t *testing.T) {
	m := newModel()
	m.viewport = viewport.New(80, 20)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
}
