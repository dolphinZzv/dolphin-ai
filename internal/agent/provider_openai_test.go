package agent

import (
	"context"
	"encoding/json"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
)

func TestNewOpenAIProvider(t *testing.T) {
	cfg := &config.LLMConfig{
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
	cfg := &config.LLMConfig{APIKey: "test"}
	p := NewOpenAIProvider(cfg)
	if p.Type() != ProviderOpenAI {
		t.Errorf("Type() = %v", ProviderOpenAI)
	}
}

func TestOpenAIBuildMessagesWithSystem(t *testing.T) {
	cfg := &config.LLMConfig{APIKey: "test"}
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
	cfg := &config.LLMConfig{APIKey: "test"}
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
	cfg := &config.LLMConfig{APIKey: "test"}
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
	cfg := &config.LLMConfig{APIKey: "test"}
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
	cfg := &config.LLMConfig{APIKey: "test"}
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
