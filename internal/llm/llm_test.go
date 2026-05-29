package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"dolphin/internal/types"
	"github.com/h2non/gock"
	"go.uber.org/zap"
)

func TestNewProvider_OpenAI(t *testing.T) {
	p := NewProvider(Config{Provider: "openai", APIKey: "key"}, zap.NewNop())
	if p.Name() != "openai" {
		t.Fatalf("expected 'openai', got '%s'", p.Name())
	}
}

func TestNewProvider_Anthropic(t *testing.T) {
	p := NewProvider(Config{Provider: "anthropic", APIKey: "key"}, zap.NewNop())
	if p.Name() != "anthropic" {
		t.Fatalf("expected 'anthropic', got '%s'", p.Name())
	}
}

func TestNewProvider_UnknownDefaultsToOpenAI(t *testing.T) {
	p := NewProvider(Config{Provider: "unknown", APIKey: "key"}, zap.NewNop())
	if p.Name() != "openai" {
		t.Fatalf("expected 'openai', got '%s'", p.Name())
	}
}

func TestOpenAIProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "test-key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Content
	}

	if content != "Hello world" {
		t.Fatalf("expected 'Hello world', got '%s'", content)
	}
}

func TestOpenAIProvider_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{
			"error": map[string]any{"message": "Invalid API key"},
		})

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "bad-key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "Invalid API key") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestOpenAIProvider_CompleteStreamHTTPErrorNoMessage(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(500).
		BodyString("Internal Server Error")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "500") {
		t.Fatalf("expected status 500 in error, got: %v", chunk.Error)
	}
}

func TestOpenAIProvider_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		ReplyError(errors.New("connection timeout"))

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "connection timeout") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestOpenAIProvider_CompleteStreamCustomBaseURL(t *testing.T) {
	defer gock.Off()

	gock.New("https://custom.example.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key", BaseURL: "https://custom.example.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Content
	}

	if content != "ok" {
		t.Fatalf("expected 'ok', got '%s'", content)
	}
}

func TestOpenAIProvider_CompleteStreamEmptyResponse(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var chunks int
	for chunk := range ch {
		chunks++
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		_ = chunk.Done
	}

	if chunks != 1 {
		t.Fatalf("expected 1 chunk (Done), got %d", chunks)
	}
}

func TestAnthropicProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3-opus", APIKey: "ant-key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	var done bool
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Content
		if chunk.Done {
			done = true
		}
	}

	if content != "Hello world" {
		t.Fatalf("expected 'Hello world', got '%s'", content)
	}
	if !done {
		t.Fatal("expected done signal")
	}
}

func TestAnthropicProvider_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(400).
		JSON(map[string]any{
			"error": map[string]any{"message": "Invalid request"},
		})

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "Invalid request") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestAnthropicProvider_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		ReplyError(errors.New("connection refused"))

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "connection refused") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestAnthropicProvider_CompleteStreamCustomBaseURL(t *testing.T) {
	defer gock.Off()

	gock.New("https://custom.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "key", BaseURL: "https://custom.anthropic.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Content
	}

	if content != "hi" {
		t.Fatalf("expected 'hi', got '%s'", content)
	}
}

func TestStreamDecoder_Decode(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"

	dec := NewStreamDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "hello" {
		t.Fatalf("expected 'hello', got '%s'", chunk.Content)
	}
	if chunk.Done {
		t.Fatal("expected not done")
	}

	chunk, err = dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != " world" {
		t.Fatalf("expected ' world', got '%s'", chunk.Content)
	}
	if !chunk.Done {
		t.Fatal("expected done (finish_reason=stop)")
	}

	chunk, err = dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Done {
		t.Fatal("expected done from [DONE]")
	}

	_, err = dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestStreamDecoder_SkipInvalidLines(t *testing.T) {
	// Lines without "data: " prefix and empty lines should be skipped.
	body := "keepalive: tick\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\nrandom text\n\ndata: [DONE]\n"

	dec := NewStreamDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "ok" {
		t.Fatalf("expected 'ok', got '%s'", chunk.Content)
	}
}

func TestStreamDecoder_EmptyBody(t *testing.T) {
	dec := NewStreamDecoder(strings.NewReader(""))

	_, err := dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAnthropicStreamDecoder_Decode(t *testing.T) {
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"message_stop\"}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "Hello" {
		t.Fatalf("expected 'Hello', got '%s'", chunk.Content)
	}

	chunk, err = dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Done {
		t.Fatal("expected done from message_stop")
	}

	_, err = dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAnthropicStreamDecoder_MessageDelta(t *testing.T) {
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	dec.Decode() // content

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Done {
		t.Fatal("expected done from message_delta")
	}
}

func TestAnthropicStreamDecoder_Error(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"rate limited\"}}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(chunk.Error.Error(), "rate limited") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestAnthropicStreamDecoder_ErrorWithoutMessage(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{}}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicStreamDecoder_SkipEventTypes(t *testing.T) {
	// message_start, content_block_start, content_block_stop, ping should be skipped.
	body := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\"}}\n\ndata: {\"type\":\"ping\"}\n\ndata: {\"type\":\"content_block_start\"}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"data\"}}\n\ndata: {\"type\":\"content_block_stop\"}\n\ndata: {\"type\":\"message_stop\"}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "data" {
		t.Fatalf("expected 'data', got '%s'", chunk.Content)
	}

	chunk, err = dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Done {
		t.Fatal("expected done")
	}
}

func TestAnthropicStreamDecoder_Thinking(t *testing.T) {
	// Anthropic standard: thinking_delta uses "text" key.
	body := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"thinking...\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"text\":\" more thinking\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-123\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Thinking != "thinking... more thinking" {
		t.Fatalf("expected 'thinking... more thinking', got '%s'", chunk.Thinking)
	}
	if chunk.ThinkingSignature != "sig-123" {
		t.Fatalf("expected signature 'sig-123', got '%s'", chunk.ThinkingSignature)
	}

	_, err = dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAnthropicStreamDecoder_Thinking_DeepSeekVariant(t *testing.T) {
	// DeepSeek variant: thinking_delta uses "thinking" key instead of "text".
	body := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\ndata: {\"type\":\"ping\"}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"用户\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"要求\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"45e11b97-c79a-4ff0-b82c-9b0b2c1df70c\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n"

	dec := NewAnthropicDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Thinking != "用户要求" {
		t.Fatalf("expected '用户要求', got '%s'", chunk.Thinking)
	}
	if chunk.ThinkingSignature != "45e11b97-c79a-4ff0-b82c-9b0b2c1df70c" {
		t.Fatalf("expected signature '45e11b97-...', got '%s'", chunk.ThinkingSignature)
	}

	_, err = dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestAnthropicStreamDecoder_EmptyBody(t *testing.T) {
	dec := NewAnthropicDecoder(strings.NewReader(""))

	_, err := dec.Decode()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestDefaultSchema_Nil(t *testing.T) {
	schema := defaultSchema(nil)
	if string(schema) != `{"type":"object"}` {
		t.Fatalf("expected '{\"type\":\"object\"}', got '%s'", string(schema))
	}
}

func TestDefaultSchema_Empty(t *testing.T) {
	schema := defaultSchema(json.RawMessage{})
	if string(schema) != `{"type":"object"}` {
		t.Fatalf("expected '{\"type\":\"object\"}', got '%s'", string(schema))
	}
}

func TestDefaultSchema_NonEmpty(t *testing.T) {
	input := json.RawMessage(`{"type":"object"}`)
	schema := defaultSchema(input)
	if string(schema) != `{"type":"object"}` {
		t.Fatalf("unexpected schema: %s", string(schema))
	}
}

func TestBuildOpenAITools(t *testing.T) {
	tools := []types.ToolDef{
		{Name: "greet", Description: "Say hello", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "echo", Description: "Echo input"},
	}
	result := buildOpenAITools(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Function.Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", result[0].Function.Name)
	}
	if result[0].Function.Description != "Say hello" {
		t.Fatalf("unexpected description: %s", result[0].Function.Description)
	}
	// nil schema should become {"type":"object"}
	if string(result[1].Function.Parameters) != `{"type":"object"}` {
		t.Fatalf("expected default schema, got %s", string(result[1].Function.Parameters))
	}
}

func TestBuildAnthropicTools(t *testing.T) {
	tools := []types.ToolDef{
		{Name: "greet", Description: "Say hello", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "echo", Description: "Echo input"},
	}
	result := buildAnthropicTools(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", result[0].Name)
	}
	// nil schema should become {"type":"object"}
	if string(result[1].InputSchema) != `{"type":"object"}` {
		t.Fatalf("expected default schema, got %s", string(result[1].InputSchema))
	}
}

func TestOpenAIBuildMessages_WithToolCalls(t *testing.T) {
	provider := &openAIProvider{cfg: Config{}, logger: zap.NewNop()}
	msgs := provider.buildOpenAIMessages(LLMRequest{
		Messages: []types.Message{
			{
				Role:    types.RoleAssistant,
				Content: "I'll call a tool",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "greet", Arguments: `{"name":"world"}`},
				},
			},
			{
				Role:       types.RoleTool,
				ToolCallID: "call-1",
				Content:    "Hello, world!",
			},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("expected 'assistant', got '%s'", msgs[0].Role)
	}
	if msgs[0].Content != nil {
		t.Fatalf("expected nil content for assistant with tool calls")
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[1].Role != "tool" {
		t.Fatalf("expected 'tool', got '%s'", msgs[1].Role)
	}
	if msgs[1].ToolCallID != "call-1" {
		t.Fatalf("expected 'call-1', got '%s'", msgs[1].ToolCallID)
	}
}

func TestAnthropicBuildMessages_WithToolCalls(t *testing.T) {
	provider := &anthropicProvider{cfg: Config{}, logger: zap.NewNop()}
	msgs := provider.buildAnthropicMessages(LLMRequest{
		Messages: []types.Message{
			{
				Role:    types.RoleAssistant,
				Content: "I'll call a tool",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "greet", Arguments: `{"name":"world"}`},
				},
			},
			{
				Role:       types.RoleTool,
				ToolCallID: "call-1",
				Content:    "Hello, world!",
			},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	var parsed []map[string]any
	if err := json.Unmarshal(msgs[0].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	// Should have text + tool_use blocks
	if len(parsed) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(parsed))
	}
	if parsed[0]["type"] != "text" {
		t.Fatalf("expected 'text', got '%s'", parsed[0]["type"])
	}
	if parsed[1]["type"] != "tool_use" {
		t.Fatalf("expected 'tool_use', got '%s'", parsed[1]["type"])
	}
	// Tool result should be a user message with tool_result block
	if err := json.Unmarshal(msgs[1].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(parsed))
	}
	if parsed[0]["type"] != "tool_result" {
		t.Fatalf("expected 'tool_result', got '%s'", parsed[0]["type"])
	}
}

func TestAnthropicBuildMessages_WithThinking(t *testing.T) {
	provider := &anthropicProvider{cfg: Config{}, logger: zap.NewNop()}
	msgs := provider.buildAnthropicMessages(LLMRequest{
		Messages: []types.Message{
			{
				Role:              types.RoleAssistant,
				Content:           "visible text",
				Thinking:          "hidden reasoning",
				ThinkingSignature: "sig-abc",
			},
		},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	var parsed []map[string]any
	if err := json.Unmarshal(msgs[0].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(parsed))
	}
	if parsed[0]["type"] != "thinking" {
		t.Fatalf("expected first block type 'thinking', got '%s'", parsed[0]["type"])
	}
	if parsed[0]["thinking"] != "hidden reasoning" {
		t.Fatalf("expected thinking 'hidden reasoning', got '%v'", parsed[0]["thinking"])
	}
	if parsed[0]["signature"] != "sig-abc" {
		t.Fatalf("expected signature 'sig-abc', got '%v'", parsed[0]["signature"])
	}
	if parsed[1]["type"] != "text" {
		t.Fatalf("expected second block type 'text', got '%s'", parsed[1]["type"])
	}
	if parsed[1]["text"] != "visible text" {
		t.Fatalf("expected text 'visible text', got '%v'", parsed[1]["text"])
	}
}

func TestStreamDecoder_DeepSeekCharByChar(t *testing.T) {
	// Simulate DeepSeek's character-by-character streaming:
	//   chunk 1: id+name + empty arguments
	//   chunks 2..N: one character per chunk (arguments JSON-escaped)
	//   final chunk: tool_calls finish_reason + usage in same chunk
	args := `{"command":"date"}`
	var b strings.Builder

	// Chunk 1: id + name + empty arguments
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_ds","type":"function","function":{"name":"shell","arguments":""}}]},"finish_reason":null}]}` + "\n\n")

	// Per-character chunks with proper JSON escaping
	for _, r := range args {
		escaped := strings.ReplaceAll(string(r), `"`, `\"`)
		b.WriteString(fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"%s"}}]},"finish_reason":null}]}`+"\n\n", escaped))
	}

	// Final chunk: tool_calls finish_reason + usage in same chunk
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"content":"","reasoning_content":null},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":3}}}` + "\n\n")

	dec := NewStreamDecoder(strings.NewReader(b.String()))

	var gotTC []types.ToolCall
	var gotTokens int
	for {
		chunk, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk.ToolCalls) > 0 {
			gotTC = chunk.ToolCalls
		}
		if chunk.InputTokens > 0 {
			gotTokens = chunk.InputTokens
		}
		if chunk.Done {
			break
		}
	}

	if len(gotTC) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(gotTC))
	}
	if gotTC[0].Name != "shell" {
		t.Fatalf("expected 'shell', got '%s'", gotTC[0].Name)
	}
	if gotTC[0].ID != "call_ds" {
		t.Fatalf("expected 'call_ds', got '%s'", gotTC[0].ID)
	}
	if gotTC[0].Arguments != args {
		t.Fatalf("expected '%s', got '%s'", args, gotTC[0].Arguments)
	}
	if gotTokens != 10 {
		t.Fatalf("expected 10 input tokens, got %d", gotTokens)
	}
}

func TestStreamDecoder_ToolCalls(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"greet","arguments":"{\"name\":\"world\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n"

	dec := NewStreamDecoder(strings.NewReader(body))

	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.ToolCalls))
	}
	if chunk.ToolCalls[0].Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", chunk.ToolCalls[0].Name)
	}
	if chunk.ToolCalls[0].Arguments != `{"name":"world"}` {
		t.Fatalf("expected '{\"name\":\"world\"}', got '%s'", chunk.ToolCalls[0].Arguments)
	}
}
