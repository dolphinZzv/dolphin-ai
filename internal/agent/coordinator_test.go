package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/session"
)

func TestReplayMessagesUserAssistant(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"hello"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"hi there"`)},
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"what time is it"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"12:00"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || string(msgs[0].Content) != `"hello"` {
		t.Errorf("msg[0] mismatch: role=%q content=%s", msgs[0].Role, string(msgs[0].Content))
	}
	if msgs[1].Role != "assistant" || string(msgs[1].Content) != `"hi there"` {
		t.Errorf("msg[1] mismatch: role=%q content=%s", msgs[1].Role, string(msgs[1].Content))
	}
}

func TestReplayMessagesWithToolResults(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"list files"`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"running"},{"type":"tool_use","id":"tc1","name":"shell","input":{"command":"ls"}}]`)},
		{Type: session.EventToolResult, ToolName: "shell", ToolResult: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tc1","content":[{"type":"text","text":"file1.txt"}]}]`)},
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"done"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msg[0] expected user")
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1] expected assistant")
	}
	// Tool result
	if msgs[2].Role != "tool" {
		t.Errorf("msg[2] expected tool, got %q", msgs[2].Role)
	}
	if msgs[3].Role != "assistant" {
		t.Errorf("msg[3] expected assistant")
	}
}

func TestReplayMessagesSkipsSystemAndToolCall(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventSystem, Content: json.RawMessage(`"system event"`)},
		{Type: session.EventToolCall, ToolName: "shell", ToolInput: json.RawMessage(`{"command":"date"}`)},
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"hello"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user message")
	}
}

func TestReplayMessagesSkipsEmptyContent(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Role: "user"}, // no content
		{Type: session.EventMessage, Role: "assistant", Content: json.RawMessage(`"ok"`)},
		{Type: session.EventToolResult}, // no result content
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected assistant message")
	}
}

func TestReplayMessagesEmptyInput(t *testing.T) {
	msgs := replayMessages(nil)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for nil input, got %d", len(msgs))
	}

	msgs = replayMessages([]session.SessionEvent{})
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty input, got %d", len(msgs))
	}
}

func TestReplayMessagesSkipsMessageWithoutRole(t *testing.T) {
	events := []session.SessionEvent{
		{Type: session.EventMessage, Content: json.RawMessage(`"no role"`)}, // missing role
		{Type: session.EventMessage, Role: "user", Content: json.RawMessage(`"has role"`)},
	}

	msgs := replayMessages(events)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"}, // 1m30s rounds to 1m
		{5 * time.Minute, "5m"},
		{70 * time.Minute, "1h10m"}, // 1h10m
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestCoordinatorToolDefinition(t *testing.T) {
	def := mcp.ToolDefinition{Name: "test"}
	tool := &handlerTool{def: def}
	if d := tool.Definition(); d.Name != "test" {
		t.Errorf("got %q", d.Name)
	}
}

func TestCoordinatorToolExecute(t *testing.T) {
	executed := false
	tool := &handlerTool{
		handler: func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			executed = true
			return &mcp.ToolResult{Content: "ok"}, nil
		},
	}
	_, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !executed {
		t.Error("handler was not called")
	}
}

func TestFormatDurationEdgeCases(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
		{-5 * time.Minute, "-300s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
