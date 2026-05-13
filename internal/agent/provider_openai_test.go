package agent

import (
	"context"
	"encoding/json"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
)

func TestNewOpenAIProvider(t *testing.T) {
	cfg := &config.ProviderConfig{
		APIKey:    "test-key",
		BaseURL:   "https://example.com/v1",
		Model:     "gpt-4o",
		MaxTokens: 4096,
	}
	p := NewOpenAIProvider(cfg)
	if p == nil {
		t.Fatal("provider is nil")
	}
	if p.model != "gpt-4o" {
		t.Errorf("model = %q", p.model)
	}
	if p.maxTok != 4096 {
		t.Errorf("maxTok = %d", p.maxTok)
	}
}

func TestOpenAIProviderType(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	if p.Type() != ProviderOpenAI {
		t.Errorf("Type() = %v", ProviderOpenAI)
	}
}

func TestOpenAIBuildMessagesWithSystem(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessages(ProviderRequest{
		System: "You are a helpful assistant.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("msg[0] role = %q", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("msg[1] role = %q", msgs[1].Role)
	}
}

func TestOpenAIBuildMessagesAssistantWithToolCall(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessages(ProviderRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"text","text":"let me search"},
					{"type":"tool_use","id":"tc1","name":"shell","input":{"command":"ls"}}
				]`),
			},
			{
				Role:    "tool",
				Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tc1","content":[{"type":"text","text":"file1.txt"}]}]`),
			},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].ID != "tc1" {
		t.Errorf("tool call ID = %q", msgs[0].ToolCalls[0].ID)
	}
	if msgs[0].ToolCalls[0].Function.Name != "shell" {
		t.Errorf("tool call name = %q", msgs[0].ToolCalls[0].Function.Name)
	}
	if msgs[1].Role != "tool" {
		t.Errorf("msg[1] role = %q", msgs[1].Role)
	}
}

func TestOpenAIBuildMessagesToolOnly(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessages(ProviderRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"tc1","name":"shell","input":{"command":"date"}}
				]`),
			},
			{
				Role:    "tool",
				Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tc1","content":[{"type":"text","text":"Mon"}]}]`),
			},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "" {
		t.Errorf("expected empty content, got %q", msgs[0].Content)
	}
}

func TestOpenAIBuildMessagesNoSystem(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessages(ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestOpenAIBuildTools(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	tools := p.buildTools([]ToolDef{
		{
			Name:        "shell",
			Description: "run a command",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Function.Name != "shell" {
		t.Errorf("tool name = %q", tools[0].Function.Name)
	}
}

func TestOpenAIBuildMessagesRawWithThinking(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessagesRaw(ProviderRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"let me think about this"},
					{"type":"text","text":"here is my answer"}
				]`),
			},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0]
	if msg["role"] != "assistant" {
		t.Errorf("role = %q", msg["role"])
	}
	if msg["content"] != "here is my answer" {
		t.Errorf("content = %q", msg["content"])
	}
	if msg["reasoning_content"] != "let me think about this" {
		t.Errorf("reasoning_content = %q", msg["reasoning_content"])
	}
}

func TestOpenAIBuildMessagesRawNoThinking(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessagesRaw(ProviderRequest{
		System: "be helpful",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
			},
		},
	})
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1]["role"] != "user" {
		t.Errorf("msg[1] role = %q", msgs[1]["role"])
	}
	if msgs[2]["role"] != "assistant" {
		t.Errorf("msg[2] role = %q", msgs[2]["role"])
	}
	if _, ok := msgs[2]["reasoning_content"]; ok {
		t.Error("unexpected reasoning_content without thinking block")
	}
}

func TestOpenAIBuildMessagesRawWithToolCall(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessagesRaw(ProviderRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"text","text":"let me check"},
					{"type":"tool_use","id":"tc1","name":"shell","input":{"cmd":"ls"}}
				]`),
			},
			{
				Role:    "tool",
				Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"tc1","content":[{"type":"text","text":"files"}]}]`),
			},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Assistant should have tool_calls
	tcs, ok := msgs[0]["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("expected 1 tool_call, got %v", tcs)
	}
	tc := tcs[0].(map[string]any)
	if tc["id"] != "tc1" {
		t.Errorf("tool_call id = %q", tc["id"])
	}
	if _, ok := msgs[1]["reasoning_content"]; ok {
		t.Error("tool message should not have reasoning_content")
	}
}

func TestOpenAIBuildMessagesRawThinkingWithToolCall(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	msgs := p.buildMessagesRaw(ProviderRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"i need to search"},
					{"type":"text","text":"let me look that up"},
					{"type":"tool_use","id":"tc1","name":"shell","input":{"cmd":"find"}}
				]`),
			},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0]
	if msg["reasoning_content"] != "i need to search" {
		t.Errorf("reasoning_content = %q", msg["reasoning_content"])
	}
	if msg["content"] != "let me look that up" {
		t.Errorf("content = %q", msg["content"])
	}
	tcs := msg["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(tcs))
	}
}

func TestOpenAIBuildMessagesRawPlainTextFallback(t *testing.T) {
	cfg := &config.ProviderConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	// Non-JSON content (plain text) should be passed through as-is
	msgs := p.buildMessagesRaw(ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"plain text"`)},
			{Role: "assistant", Content: json.RawMessage(`"direct response"`)},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1]["content"] != "\"direct response\"" {
		t.Errorf("content = %q", msgs[1]["content"])
	}
}

func TestHandlerToolDefinition(t *testing.T) {
	def := mcp.ToolDefinition{Name: "test-tool"}
	tool := &handlerTool{def: def}
	if d := tool.Definition(); d.Name != "test-tool" {
		t.Errorf("got %q", d.Name)
	}
}

func TestHandlerToolExecute(t *testing.T) {
	tool := &handlerTool{
		handler: func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			return &mcp.ToolResult{Content: "handled"}, nil
		},
	}
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content != "handled" {
		t.Errorf("got %q", result.Content)
	}
}
