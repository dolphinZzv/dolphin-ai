package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/h2non/gock"
	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/types"
)

func TestBuildMessages_WithToolCalls(t *testing.T) {
	msgs := BuildMessages(llm.LLMRequest{
		Messages: []types.Message{
			{
				Role:  types.RoleAssistant,
				Parts: []types.ContentPart{types.TextPart("I'll call a tool")},
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "greet", Arguments: `{"name":"world"}`},
				},
			},
			{Role: types.RoleTool, ToolCallID: "call-1", Parts: []types.ContentPart{types.TextPart("Hello, world!")}},
		},
	}, zap.NewNop())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	var parsed []map[string]any
	if err := json.Unmarshal(msgs[0].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0]["type"] != "text" || parsed[1]["type"] != "tool_use" {
		t.Fatalf("unexpected blocks: %+v", parsed)
	}
	if err := json.Unmarshal(msgs[1].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0]["type"] != "tool_result" {
		t.Fatalf("expected tool_result, got %+v", parsed)
	}
}

func TestBuildMessages_WithThinking(t *testing.T) {
	msgs := BuildMessages(llm.LLMRequest{
		Messages: []types.Message{{
			Role:              types.RoleAssistant,
			Parts:             []types.ContentPart{types.TextPart("visible text")},
			Thinking:          "hidden reasoning",
			ThinkingSignature: "sig-abc",
		}},
	}, zap.NewNop())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	var parsed []map[string]any
	if err := json.Unmarshal(msgs[0].Content, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0]["type"] != "thinking" || parsed[0]["thinking"] != "hidden reasoning" || parsed[0]["signature"] != "sig-abc" {
		t.Fatalf("unexpected: %+v", parsed)
	}
	if parsed[1]["type"] != "text" || parsed[1]["text"] != "visible text" {
		t.Fatalf("unexpected: %+v", parsed[1])
	}
}

func TestBuildMessages_WithImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "x.png")
	if err := os.WriteFile(imgPath, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := BuildMessages(llm.LLMRequest{
		Messages: []types.Message{{
			Role: types.RoleUser,
			Parts: []types.ContentPart{
				types.TextPart("describe"),
				{Type: types.PartImage, Path: imgPath, MIME: "image/png", Filename: "x.png"},
			},
		}},
	}, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	var blocks []map[string]any
	if err := json.Unmarshal(msgs[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[1]["type"] != "image" {
		t.Errorf("expected type image, got %v", blocks[1]["type"])
	}
	src, ok := blocks[1]["source"].(map[string]any)
	if !ok {
		t.Fatalf("expected source map, got %T", blocks[1]["source"])
	}
	if src["type"] != "base64" {
		t.Errorf("expected source.type base64, got %v", src["type"])
	}
	if src["media_type"] != "image/png" {
		t.Errorf("expected source.media_type image/png, got %v", src["media_type"])
	}
}

func TestBuildTools(t *testing.T) {
	tools := []types.ToolDef{
		{Name: "greet", Description: "Say hello", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "echo", Description: "Echo input"},
	}
	result := BuildTools(tools)
	if len(result) != 2 || result[0].Name != "greet" || string(result[1].InputSchema) != `{"type":"object"}` {
		t.Fatalf("unexpected: %+v", result)
	}
}

func TestChunkDecoder_Decode(t *testing.T) {
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"message_stop\"}\n"
	dec := NewChunkDecoder(strings.NewReader(body))

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
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestChunkDecoder_MessageDelta(t *testing.T) {
	body := "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	_, _ = dec.Decode()
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !chunk.Done {
		t.Fatal("expected done from message_delta")
	}
}

func TestChunkDecoder_Error(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"rate limited\"}}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Error == nil || !strings.Contains(chunk.Error.Error(), "rate limited") {
		t.Fatalf("unexpected: %v", chunk.Error)
	}
}

func TestChunkDecoder_ErrorWithoutMessage(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{}}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Error == nil {
		t.Fatal("expected error")
	}
}

func TestChunkDecoder_SkipEventTypes(t *testing.T) {
	body := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\"}}\n\ndata: {\"type\":\"ping\"}\n\ndata: {\"type\":\"content_block_start\"}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"data\"}}\n\ndata: {\"type\":\"content_block_stop\"}\n\ndata: {\"type\":\"message_stop\"}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
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

func TestChunkDecoder_Thinking(t *testing.T) {
	body := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"thinking...\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"text\":\" more thinking\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-123\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Thinking != "thinking... more thinking" || chunk.ThinkingSignature != "sig-123" {
		t.Fatalf("unexpected: %+v", chunk)
	}
	_, err = dec.Decode()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestChunkDecoder_Thinking_DeepSeekVariant(t *testing.T) {
	body := "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\ndata: {\"type\":\"ping\"}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"用户\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"要求\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"45e11b97-c79a-4ff0-b82c-9b0b2c1df70c\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Thinking != "用户要求" || chunk.ThinkingSignature != "45e11b97-c79a-4ff0-b82c-9b0b2c1df70c" {
		t.Fatalf("unexpected: %+v", chunk)
	}
}

func TestChunkDecoder_EmptyBody(t *testing.T) {
	dec := NewChunkDecoder(strings.NewReader(""))
	_, err := dec.Decode()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestChatURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"", "https://api.anthropic.com/v1/messages"},
		{"https://custom.example.com", "https://custom.example.com/v1/messages"},
	}
	for _, tt := range tests {
		if got := ChatURL(tt.baseURL); got != tt.want {
			t.Errorf("ChatURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestModelsURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{"default", "", "https://api.anthropic.com/v1/models"},
		{"custom", "https://custom.anthropic.com", "https://custom.anthropic.com/v1/models"},
		{"deepseek anthropic", "https://api.deepseek.com/anthropic", "https://api.deepseek.com/anthropic/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelsURL(tt.baseURL); got != tt.expected {
				t.Errorf("ModelsURL(%q) = %q, want %q", tt.baseURL, got, tt.expected)
			}
		})
	}
}

func TestBuildRequest(t *testing.T) {
	msgs := []Message{{Role: "user", Content: json.RawMessage(`"hello"`)}}
	req := llm.LLMRequest{
		Messages:  []types.Message{{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("hi")}}},
		System:    "You are helpful.",
		MaxTokens: 100,
		Stream:    true,
	}

	t.Run("basic request", func(t *testing.T) {
		data, err := BuildRequest("claude-3", msgs, llm.Config{}, req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		if body["system"] != "You are helpful." || body["stream"] != true {
			t.Errorf("unexpected: %+v", body)
		}
	})

	t.Run("with tools", func(t *testing.T) {
		r := req
		r.Tools = []types.ToolDef{{Name: "greet", Description: "Say hello"}}
		data, err := BuildRequest("claude-3", msgs, llm.Config{}, r)
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
		r := req
		r.Stop = []string{"\n"}
		data, err := BuildRequest("claude-3", msgs, llm.Config{}, r)
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
		data, err := BuildRequest("claude-3", msgs, llm.Config{}, req)
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
		data, err := BuildRequest("claude-3", msgs, llm.Config{Temperature: 0.9}, llm.LLMRequest{
			Messages:    []types.Message{{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("hi")}}},
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

func newHTTPRequest(t *testing.T, url string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "ant-key")
	req.Header.Set("anthropic-version", "2023-06-01")
	return req
}

func TestDoStream(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hi\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", []byte(`{}`))
	ch, err := proto.DoStream(context.Background(), req, 5*time.Second, NewChunkDecoder, DecodeError, zap.NewNop())
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

func TestDoStream_HTTPError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad request"}})

	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", []byte(`{}`))
	ch, err := proto.DoStream(context.Background(), req, 5*time.Second, NewChunkDecoder, DecodeError, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error == nil || !strings.Contains(chunk.Error.Error(), "bad request") {
		t.Fatalf("unexpected: %v", chunk.Error)
	}
}

func TestDoStream_NetworkError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		ReplyError(errors.New("refused"))

	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", []byte(`{}`))
	ch, err := proto.DoStream(context.Background(), req, 5*time.Second, NewChunkDecoder, DecodeError, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if chunk := <-ch; chunk.Error == nil {
		t.Fatal("expected error")
	}
}

func TestDoComplete(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		JSON(map[string]any{
			"type": "message", "role": "assistant",
			"content": []map[string]any{{"type": "text", "text": "Hello from Claude"}},
			"usage":   map[string]any{"input_tokens": 10, "output_tokens": 25, "cache_creation_input_tokens": 5, "cache_read_input_tokens": 3},
		})

	body, _ := BuildRequest("claude-3", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "claude-3", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
	if !chunk.Done || chunk.Content != "Hello from Claude" {
		t.Errorf("unexpected: %+v", chunk)
	}
	if chunk.InputTokens != 10 || chunk.OutputTokens != 25 || chunk.CacheCreationInputTokens != 5 || chunk.CacheReadInputTokens != 3 {
		t.Errorf("unexpected tokens: %+v", chunk)
	}
}

func TestDoComplete_WithToolUse(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		JSON(map[string]any{
			"type": "message", "role": "assistant",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me check...", "signature": "sig123"},
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]any{"location": "NYC"}},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 15},
		})

	body, _ := BuildRequest("claude-3", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "claude-3", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
	if chunk.Thinking != "Let me check..." || chunk.ThinkingSignature != "sig123" || len(chunk.ToolCalls) != 1 || chunk.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected: %+v", chunk)
	}
}

func TestDoComplete_Error(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad request"}})

	body, _ := BuildRequest("claude-3", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "claude-3", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.anthropic.com/v1/messages", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error == nil || !strings.Contains(chunk.Error.Error(), "bad request") {
		t.Fatalf("unexpected: %v", chunk.Error)
	}
}

func TestDiscoverModels(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "claude-3-opus-20240229"}, {"id": "claude-3-sonnet-20240229"}}})

	cfg := llm.Config{Vendor: "anthropic", APIType: "anthropic", APIKey: "sk-ant-test", BaseURL: "https://api.anthropic.com"}
	models, err := DiscoverModels(context.Background(), cfg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(models) != 2 || models[0].Name != "claude-3-opus-20240229" || models[0].APIType != "anthropic" {
		t.Errorf("unexpected: %+v", models)
	}
}

func TestDiscoverModels_HTTPError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Get("/v1/models").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad request"}})

	cfg := llm.Config{APIKey: "bad-key", BaseURL: "https://api.anthropic.com"}
	if _, err := DiscoverModels(context.Background(), cfg); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeError(t *testing.T) {
	t.Run("valid error", func(t *testing.T) {
		err := DecodeError(400, []byte(`{"error":{"message":"rate limited"}}`))
		if err == nil || err.Error() != "llm: rate limited (status 400)" {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("invalid json returns nil", func(t *testing.T) {
		err := DecodeError(400, []byte(`not json`))
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("empty message returns nil", func(t *testing.T) {
		err := DecodeError(400, []byte(`{"error":{"message":""}}`))
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("non-error body returns nil", func(t *testing.T) {
		err := DecodeError(400, []byte(`{"id":"msg-1"}`))
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestDefaultSchema(t *testing.T) {
	t.Run("empty schema returns default", func(t *testing.T) {
		s := DefaultSchema(nil)
		if string(s) != `{"type":"object"}` {
			t.Fatalf("unexpected: %s", string(s))
		}
	})

	t.Run("non-empty schema returned as-is", func(t *testing.T) {
		s := DefaultSchema(json.RawMessage(`{"type":"string"}`))
		if string(s) != `{"type":"string"}` {
			t.Fatalf("unexpected: %s", string(s))
		}
	})
}
