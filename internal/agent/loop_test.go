package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"dolphinzZ/internal/config"
	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/session"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	responses []*ProviderResponse
	callIndex int
}

func (m *mockProvider) Type() ProviderType { return "openai" }
func (m *mockProvider) Complete(_ context.Context, _ ProviderRequest) (*ProviderResponse, error) {
	if m.callIndex >= len(m.responses) {
		return &ProviderResponse{
			Content: TextContent("done"),
		}, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}
func (m *mockProvider) CompleteStream(_ context.Context, _ ProviderRequest) (<-chan StreamChunk, error) {
	if m.callIndex >= len(m.responses) {
		ch := make(chan StreamChunk, 1)
		ch <- StreamChunk{Done: true}
		close(ch)
		return ch, nil
	}
	resp := m.responses[m.callIndex]
	m.callIndex++

	ch := make(chan StreamChunk, 10)
	go func() {
		defer close(ch)

		// Stream text from content blocks
		var blocks []map[string]any
		if err := json.Unmarshal(resp.Content, &blocks); err == nil {
			for _, b := range blocks {
				if t, ok := b["text"].(string); ok && t != "" {
					ch <- StreamChunk{Content: TextContent(t)}
				}
			}
		}

		// Stream tool calls
		for _, tc := range resp.ToolCalls {
			ch <- StreamChunk{
				ToolCallBegin: &ToolCallBegin{ID: tc.ID, Name: tc.Name},
			}
			if len(tc.Arguments) > 0 {
				ch <- StreamChunk{ToolCallDelta: string(tc.Arguments)}
			}
		}

		if resp.Usage != nil {
			ch <- StreamChunk{Usage: resp.Usage}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// mockIO implements UserIO for testing.
type mockIO struct {
	lines   []string
	readIdx int
	writes  strings.Builder
	readErr error
}

func (m *mockIO) ReadLine() (string, error) {
	if m.readIdx >= len(m.lines) {
		return "", fmt.Errorf("no more input")
	}
	line := m.lines[m.readIdx]
	m.readIdx++
	return line, m.readErr
}
func (m *mockIO) WriteLine(s string) error {
	m.writes.WriteString(s + "\n")
	return nil
}
func (m *mockIO) WriteString(s string) error {
	m.writes.WriteString(s)
	return nil
}

// mockTool implements mcp.Tool for testing.
type mockTool struct {
	name string
}

func (t *mockTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        t.name,
		Description: "mock tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}
func (t *mockTool) Execute(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{Content: "mock result"}, nil
}

func newTestAgent(cfg *config.Config, provider Provider) *Agent {
	sessMgr := session.NewManager(cfg.Session.Dir)
	toolReg := mcp.NewRegistry(cfg)
	toolReg.Register(&mockTool{name: "test_tool"})
	return &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   provider,
		ctxBuilder: NewContextBuilder(),
	}
}

func TestCompressHistoryBelowThreshold(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{Sess: sess, Messages: []Message{
		{Role: "user", Content: TextContent("hi")},
		{Role: "assistant", Content: TextContent("hello")},
	}}

	agt.compressHistory(state)
	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(state.Messages))
	}
}

func TestCompressHistoryDropsOldMessages(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	msgs := []Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"a"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"b"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"c"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"d"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"e"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"f"}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"g"}]`)},
	}
	state := &LoopState{Sess: sess, Messages: msgs}

	agt.compressHistory(state)
	if len(state.Messages) >= len(msgs) {
		t.Error("expected messages to be compressed")
	}
}

func TestCompressHistoryPreservesLastSix(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100
	cfg.Session.Dir = t.TempDir()
	agt := newTestAgent(cfg, &mockProvider{})

	sess, _ := agt.sessMgr.NewSession(10)
	msgs := make([]Message, 6)
	for i := 0; i < 6; i++ {
		msgs[i] = Message{
			Role:    []string{"user", "assistant"}[i%2],
			Content: json.RawMessage(`[{"type":"text","text":"x"}]`),
		}
	}
	state := &LoopState{Sess: sess, Messages: msgs}

	agt.compressHistory(state)
	if len(state.Messages) != 6 {
		t.Errorf("expected 6 messages (<=6), got %d", len(state.Messages))
	}
}

func TestRunTurnNoToolCalls(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    TextContent("hello from LLM"),
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("say hi")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Should have added the assistant response
	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(state.Messages))
	}

	if !strings.Contains(io.writes.String(), "hello from LLM") {
		t.Errorf("expected output to contain 'hello from LLM', got: %s", io.writes.String())
	}
}

func TestRunTurnWithToolCall(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	// First response: tool call, second: final text
	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    json.RawMessage(`[{"type":"text","text":"calling tool"},{"type":"tool_use","id":"tu_1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
			{
				Content:    TextContent("tool result received"),
				Usage:      &Usage{InputTokens: 20, OutputTokens: 10},
				StopReason: "end_turn",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("do something")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Should have user + assistant(tool call) + tool(result) + assistant(final) = 4
	if len(state.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(state.Messages))
	}

	output := io.writes.String()
	if !strings.Contains(output, "tool result received") {
		t.Errorf("expected final output, got: %s", output)
	}
}

func TestRunTurnTruncatesLargeResult(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.MaxContextTokens = 100000
	cfg.Session.Dir = t.TempDir()

	prov := &mockProvider{
		responses: []*ProviderResponse{
			{
				Content:    json.RawMessage(`[{"type":"text","text":"calling"},{"type":"tool_use","id":"tu_1","name":"test_tool","input":{}}]`),
				ToolCalls:  []ToolCall{{ID: "tu_1", Name: "test_tool", Arguments: json.RawMessage(`{}`)}},
				Usage:      &Usage{InputTokens: 10, OutputTokens: 5},
				StopReason: "tool_use",
			},
		},
	}
	agt := newTestAgent(cfg, prov)

	// Override tool to return large result
	agt.toolReg.Register(&mockToolLargeResult{})

	sess, _ := agt.sessMgr.NewSession(10)
	state := &LoopState{
		Sess: sess,
		Messages: []Message{
			{Role: "user", Content: TextContent("big result")},
		},
		Turn: 1,
	}
	io := &mockIO{}

	err := agt.runTurn(context.Background(), state, "system prompt", io)
	if err != nil {
		t.Fatalf("runTurn error: %v", err)
	}

	// Find the tool result message and check it was truncated
	for _, m := range state.Messages {
		if m.Role == "tool" {
			var blocks []map[string]any
			json.Unmarshal(m.Content, &blocks)
			for _, b := range blocks {
				if b["type"] == "tool_result" {
					content := b["content"]
					contentStr, _ := json.Marshal(content)
					if !strings.Contains(string(contentStr), "truncated") {
						t.Error("expected truncation notice in tool result")
					}
				}
			}
		}
	}
}

// mockToolLargeResult returns a result that exceeds the truncation limit.
type mockToolLargeResult struct{}

func (t *mockToolLargeResult) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{Name: "test_tool", Description: "large result", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (t *mockToolLargeResult) Execute(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{Content: strings.Repeat("x", 5000)}, nil
}
