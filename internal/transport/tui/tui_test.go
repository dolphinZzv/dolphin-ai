package tui

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/transport"
	"dolphin/internal/types"
)

func TestNewTUI(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
	if tui.ID() != "tui" {
		t.Errorf("expected 'tui', got %q", tui.ID())
	}
}

func TestTUI_Context(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	if tui.Context() != "" {
		t.Errorf("expected empty context, got %q", tui.Context())
	}
}

func TestTUI_Tools(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	tools := tui.Tools()
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}

func TestTUI_Flush(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	if err := tui.Flush(); err != nil {
		t.Errorf("Flush returned error: %v", err)
	}
}

func TestTUI_Capability(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
	// Write before Start (nil program) should not panic.
	err := tui.Write(context.Background(), "hello")
	if err != nil {
		t.Errorf("Write returned error: %v", err)
	}
}

func TestTUI_WriteThinkingNilProgram(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	err := tui.WriteThinking(context.Background(), "thinking...")
	if err != nil {
		t.Errorf("WriteThinking returned error: %v", err)
	}
}

func TestTUI_WriteToolCallNilProgram(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	err := tui.WriteToolCall(context.Background(), types.ToolCall{
		ID:   "call_1",
		Name: "test_tool",
	})
	if err != nil {
		t.Errorf("WriteToolCall returned error: %v", err)
	}
}

func TestTUI_WriteToolResultNilProgram(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	err := tui.WriteToolResult(context.Background(), types.ToolResult{
		ToolCallID: "call_1",
		Content:    "result",
	})
	if err != nil {
		t.Errorf("WriteToolResult returned error: %v", err)
	}
}

func TestTUI_ReadContextCancel(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tui.Read(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestTUI_ReadCloseCancel(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	if err := tui.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := tui.Read(context.Background())
	if err == nil {
		t.Error("expected error after Close")
	}
}

func TestTUI_Close(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
	err := tui.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestTUI_RequestPermissionContextCancel(t *testing.T) {
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
	tui := NewTUI("dark", "", true, true)
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
