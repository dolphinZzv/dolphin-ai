package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"dolphin/internal/types"
	"github.com/h2non/gock"
	"go.uber.org/zap"
)

func TestOpenAIBuildMessages_WithToolCalls(t *testing.T) {
	msgs := BuildOpenAIMessages(LLMRequest{
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
	}, zap.NewNop())
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
	msgs := BuildAnthropicMessages(LLMRequest{
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
	}, zap.NewNop())
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
	msgs := BuildAnthropicMessages(LLMRequest{
		Messages: []types.Message{
			{
				Role:              types.RoleAssistant,
				Content:           "visible text",
				Thinking:          "hidden reasoning",
				ThinkingSignature: "sig-abc",
			},
		},
	}, zap.NewNop())
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

func TestBuildOpenAITools(t *testing.T) {
	tools := []types.ToolDef{
		{Name: "greet", Description: "Say hello", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "echo", Description: "Echo input"},
	}
	result := BuildOpenAITools(tools)
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
	result := BuildAnthropicTools(tools)
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

	_, _ = dec.Decode() // content

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
		fmt.Fprintf(&b, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"%s"}}]},"finish_reason":null}]}`+"\n\n", escaped)
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

func TestOpenAIChatURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"", "https://api.openai.com/v1/chat/completions"},
		{"https://custom.example.com", "https://custom.example.com/v1/chat/completions"},
		{"https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
		{"https://api.deepseek.com/v1", "https://api.deepseek.com/v1/chat/completions"},
	}
	for _, tt := range tests {
		got := OpenAIChatURL(tt.baseURL)
		if got != tt.want {
			t.Errorf("OpenAIChatURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestAnthropicChatURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"", "https://api.anthropic.com/v1/messages"},
		{"https://custom.example.com", "https://custom.example.com/v1/messages"},
	}
	for _, tt := range tests {
		got := AnthropicChatURL(tt.baseURL)
		if got != tt.want {
			t.Errorf("AnthropicChatURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestBuildOpenAIRequest(t *testing.T) {
	msgs := []OpenAIMessage{{Role: "user", Content: "hello"}}
	req := LLMRequest{Model: "gpt-4", Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}}, MaxTokens: 100}

	t.Run("default temperature", func(t *testing.T) {
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["temperature"] != 1.0 {
			t.Errorf("expected default temperature 1.0, got %v", body["temperature"])
		}
		if body["stream"] != true {
			t.Errorf("expected stream true")
		}
	})

	t.Run("custom temperature", func(t *testing.T) {
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{Temperature: 0.5}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["temperature"] != 0.5 {
			t.Errorf("expected 0.5, got %v", body["temperature"])
		}
	})

	t.Run("req temperature overrides cfg", func(t *testing.T) {
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{Temperature: 0.9}, LLMRequest{
			Messages:    []types.Message{{Role: types.RoleUser, Content: "hi"}},
			MaxTokens:   100,
			Temperature: 0.3,
		})
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["temperature"] != 0.3 {
			t.Errorf("expected 0.3, got %v", body["temperature"])
		}
	})

	t.Run("with tools", func(t *testing.T) {
		req := LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			Tools:     []types.ToolDef{{Name: "greet", Description: "Say hello"}},
			MaxTokens: 100,
		}
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if _, ok := body["tools"]; !ok {
			t.Error("expected tools in body")
		}
	})

	t.Run("with stop", func(t *testing.T) {
		req := LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			Stop:      []string{"\n"},
			MaxTokens: 100,
		}
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["stop"] == nil {
			t.Error("expected stop field")
		}
	})

	t.Run("with top_p", func(t *testing.T) {
		req := LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			TopP:      0.9,
			MaxTokens: 100,
		}
		data, err := BuildOpenAIRequest("gpt-4", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["top_p"] != 0.9 {
			t.Errorf("expected top_p 0.9, got %v", body["top_p"])
		}
	})
}

func TestBuildAnthropicRequest(t *testing.T) {
	msgs := []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}}
	req := LLMRequest{
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		System:    "You are helpful.",
		MaxTokens: 100,
	}

	t.Run("basic request", func(t *testing.T) {
		data, err := BuildAnthropicRequest("claude-3", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["system"] != "You are helpful." {
			t.Errorf("expected system prompt")
		}
		if body["stream"] != true {
			t.Errorf("expected stream true")
		}
	})

	t.Run("with tools", func(t *testing.T) {
		req := LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			System:    "You are helpful.",
			MaxTokens: 100,
			Tools:     []types.ToolDef{{Name: "greet", Description: "Say hello"}},
		}
		data, err := BuildAnthropicRequest("claude-3", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if _, ok := body["tools"]; !ok {
			t.Error("expected tools in body")
		}
	})

	t.Run("with stop", func(t *testing.T) {
		req := LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			System:    "You are helpful.",
			Stop:      []string{"\n"},
			MaxTokens: 100,
		}
		data, err := BuildAnthropicRequest("claude-3", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["stop_sequences"] == nil {
			t.Error("expected stop_sequences field")
		}
	})

	t.Run("default temperature", func(t *testing.T) {
		data, err := BuildAnthropicRequest("claude-3", msgs, Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["temperature"] != 1.0 {
			t.Errorf("expected default temperature 1.0, got %v", body["temperature"])
		}
	})

	t.Run("req temperature overrides cfg", func(t *testing.T) {
		data, err := BuildAnthropicRequest("claude-3", msgs, Config{Temperature: 0.9}, LLMRequest{
			Messages:    []types.Message{{Role: types.RoleUser, Content: "hi"}},
			MaxTokens:   100,
			Temperature: 0.3,
		})
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["temperature"] != 0.3 {
			t.Errorf("expected 0.3, got %v", body["temperature"])
		}
	})
}

func TestStreamOpenAI(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n")

	body := []byte(`{"model":"gpt-4","stream":true}`)
	ch, err := StreamOpenAI(context.Background(), "https://api.openai.com/v1/chat/completions", "test-key", nil, body, time.Second*5, zap.NewNop())
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
	if content != "hello" {
		t.Fatalf("expected 'hello', got '%s'", content)
	}
}

func TestStreamOpenAI_HTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "bad key"}})

	body := []byte(`{}`)
	ch, err := StreamOpenAI(context.Background(), "https://api.openai.com/v1/chat/completions", "bad-key", nil, body, time.Second*5, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(chunk.Error.Error(), "bad key") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestStreamOpenAI_NetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		ReplyError(errors.New("dial timeout"))

	body := []byte(`{}`)
	ch, err := StreamOpenAI(context.Background(), "https://api.openai.com/v1/chat/completions", "key", nil, body, time.Second*5, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
}

func TestStreamAnthropic(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	body := []byte(`{}`)
	ch, err := StreamAnthropic(context.Background(), "https://api.anthropic.com/v1/messages", "ant-key", nil, body, time.Second*5, zap.NewNop())
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

func TestStreamAnthropic_HTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad request"}})

	body := []byte(`{}`)
	ch, err := StreamAnthropic(context.Background(), "https://api.anthropic.com/v1/messages", "key", nil, body, time.Second*5, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(chunk.Error.Error(), "bad request") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestStreamAnthropic_NetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		ReplyError(errors.New("refused"))

	body := []byte(`{}`)
	ch, err := StreamAnthropic(context.Background(), "https://api.anthropic.com/v1/messages", "key", nil, body, time.Second*5, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
}

func TestBuildOpenAITools_Empty(t *testing.T) {
	result := BuildOpenAITools(nil)
	if result != nil {
		t.Fatal("expected nil for empty tools")
	}
}

// ---------------------------------------------------------------------------
// Root-level provider tests
// ---------------------------------------------------------------------------

func TestRootOpenAIProvider_Name(t *testing.T) {
	p := &openAIProvider{}
	if p.Name() != "openai" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestRootOpenAIProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &openAIProvider{
			cfg: Config{
				Models: []ModelConfig{
					{Name: "gpt-4", Model: "gpt-4", Provider: "openai"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "gpt-4" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model when cfg.Models is empty", func(t *testing.T) {
		p := &openAIProvider{
			cfg: Config{
				Model:       "gpt-4",
				MaxTokens:   4096,
				Temperature: 0.7,
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].Name != "gpt-4" {
			t.Errorf("Name = %q", models[0].Name)
		}
	})
}

func TestRootOpenAIProvider_chatURL(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("")
		if url != "https://api.openai.com/v1/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("https://custom.api.com")
		if url != "https://custom.api.com/v1/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})
}

func TestRootOpenAIProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "sk-key", BaseURL: "https://api.openai.com"},
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

func TestRootOpenAIProvider_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "Invalid API key"}})

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "bad-key", BaseURL: "https://api.openai.com"},
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

func TestRootOpenAIProvider_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		ReplyError(errors.New("connection timeout"))

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key", BaseURL: "https://api.openai.com"},
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
}

func TestRootOpenAIProvider_CompleteStreamEmptyResponse(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("")

	provider := &openAIProvider{
		cfg:    Config{Model: "gpt-4", APIKey: "key", BaseURL: "https://api.openai.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if chunk := <-ch; chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
}

func TestRootAnthropicProvider_Name(t *testing.T) {
	p := &anthropicProvider{}
	if p.Name() != "anthropic" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestRootAnthropicProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: Config{
				Models: []ModelConfig{
					{Name: "claude-3", Model: "claude-3", Provider: "anthropic"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "claude-3" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model when cfg.Models is empty", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: Config{
				Model:       "claude-3",
				MaxTokens:   8192,
				Temperature: 0.7,
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].Name != "claude-3" {
			t.Errorf("Name = %q", models[0].Name)
		}
	})
}

func TestRootAnthropicProvider_chatURL(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		p := &anthropicProvider{}
		url := p.chatURL("")
		if url != "https://api.anthropic.com/v1/messages" {
			t.Errorf("unexpected URL: %s", url)
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p := &anthropicProvider{}
		url := p.chatURL("https://custom.anthropic.com")
		if url != "https://custom.anthropic.com/v1/messages" {
			t.Errorf("unexpected URL: %s", url)
		}
	})
}

func TestRootAnthropicProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "sk-key", BaseURL: "https://api.anthropic.com"},
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

func TestRootAnthropicProvider_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad request"}})

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "key", BaseURL: "https://api.anthropic.com"},
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
	if !strings.Contains(chunk.Error.Error(), "bad request") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestRootAnthropicProvider_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		ReplyError(errors.New("refused"))

	provider := &anthropicProvider{
		cfg:    Config{Model: "claude-3", APIKey: "key", BaseURL: "https://api.anthropic.com"},
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
}
