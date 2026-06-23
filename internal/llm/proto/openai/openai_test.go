package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func TestBuildTools(t *testing.T) {
	tools := []types.ToolDef{
		{Name: "greet", Description: "Say hello", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "echo", Description: "Echo input"},
	}
	result := BuildTools(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Function.Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", result[0].Function.Name)
	}
	if string(result[1].Function.Parameters) != `{"type":"object"}` {
		t.Fatalf("expected default schema, got %s", string(result[1].Function.Parameters))
	}
}

func TestBuildTools_Empty(t *testing.T) {
	if result := BuildTools(nil); result != nil {
		t.Fatal("expected nil for empty tools")
	}
}

func TestChunkDecoder_Decode(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n"

	dec := NewChunkDecoder(strings.NewReader(body))

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
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestChunkDecoder_SkipInvalidLines(t *testing.T) {
	body := "keepalive: tick\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\nrandom text\n\ndata: [DONE]\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Content != "ok" {
		t.Fatalf("expected 'ok', got '%s'", chunk.Content)
	}
}

func TestChunkDecoder_EmptyBody(t *testing.T) {
	dec := NewChunkDecoder(strings.NewReader(""))
	_, err := dec.Decode()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestChunkDecoder_DeepSeekCharByChar(t *testing.T) {
	args := `{"command":"date"}`
	var b strings.Builder
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_ds","type":"function","function":{"name":"shell","arguments":""}}]},"finish_reason":null}]}` + "\n\n")
	for _, r := range args {
		escaped := strings.ReplaceAll(string(r), `"`, `\"`)
		fmt.Fprintf(&b, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"%s"}}]},"finish_reason":null}]}`+"\n\n", escaped)
	}
	b.WriteString(`data: {"choices":[{"index":0,"delta":{"content":"","reasoning_content":null},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":3}}}` + "\n\n")

	dec := NewChunkDecoder(strings.NewReader(b.String()))
	var gotTC []types.ToolCall
	var gotTokens int
	for {
		chunk, err := dec.Decode()
		if errors.Is(err, io.EOF) {
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
	if len(gotTC) != 1 || gotTC[0].Name != "shell" || gotTC[0].ID != "call_ds" || gotTC[0].Arguments != args {
		t.Fatalf("unexpected tool call: %+v", gotTC)
	}
	if gotTokens != 10 {
		t.Fatalf("expected 10 input tokens, got %d", gotTokens)
	}
}

func TestChunkDecoder_ToolCalls(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"greet","arguments":"{\"name\":\"world\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n"
	dec := NewChunkDecoder(strings.NewReader(body))
	chunk, err := dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.ToolCalls) != 1 || chunk.ToolCalls[0].Name != "greet" || chunk.ToolCalls[0].Arguments != `{"name":"world"}` {
		t.Fatalf("unexpected: %+v", chunk.ToolCalls)
	}
}

func TestChatURL(t *testing.T) {
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
		{"default", "", "https://api.openai.com/v1/models"},
		{"custom root", "https://custom.api.com", "https://custom.api.com/v1/models"},
		{"with /v1", "https://api.openai.com/v1", "https://api.openai.com/v1/models"},
		{"with /v3", "https://ark.cn-beijing.volces.com/api/v3", "https://ark.cn-beijing.volces.com/api/v3/models"},
		{"with plan/v3", "https://ark.cn-beijing.volces.com/api/plan/v3", "https://ark.cn-beijing.volces.com/api/plan/v3/models"},
		{"trailing slash", "https://api.openai.com/", "https://api.openai.com/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelsURL(tt.baseURL); got != tt.expected {
				t.Errorf("ModelsURL(%q) = %q, want %q", tt.baseURL, got, tt.expected)
			}
		})
	}
}

func TestDefaultSchema(t *testing.T) {
	if s := DefaultSchema(nil); string(s) != `{"type":"object"}` {
		t.Fatalf("got %s", s)
	}
	if s := DefaultSchema(json.RawMessage{}); string(s) != `{"type":"object"}` {
		t.Fatalf("got %s", s)
	}
	if s := DefaultSchema(json.RawMessage(`{"type":"object"}`)); string(s) != `{"type":"object"}` {
		t.Fatalf("got %s", s)
	}
}

func TestBuildRequest(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	req := llm.LLMRequest{Model: "gpt-4", Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}}, MaxTokens: 100, Stream: true}

	t.Run("default temperature", func(t *testing.T) {
		data, err := BuildRequest("gpt-4", msgs, llm.Config{}, req)
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
		data, err := BuildRequest("gpt-4", msgs, llm.Config{Temperature: 0.5}, req)
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
		data, err := BuildRequest("gpt-4", msgs, llm.Config{Temperature: 0.9}, llm.LLMRequest{
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
		r := llm.LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			Tools:     []types.ToolDef{{Name: "greet", Description: "Say hello"}},
			MaxTokens: 100,
		}
		data, err := BuildRequest("gpt-4", msgs, llm.Config{}, r)
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
		r := llm.LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			Stop:      []string{"\n"},
			MaxTokens: 100,
		}
		data, err := BuildRequest("gpt-4", msgs, llm.Config{}, r)
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
		r := llm.LLMRequest{
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
			TopP:      0.9,
			MaxTokens: 100,
		}
		data, err := BuildRequest("gpt-4", msgs, llm.Config{}, r)
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

// newHTTPRequest builds a POST request with Bearer auth for stream/complete tests.
func newHTTPRequest(t *testing.T, url string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	return req
}

func TestDoStream(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n")

	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", []byte(`{"model":"gpt-4","stream":true}`))
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
	if content != "hello" {
		t.Fatalf("expected 'hello', got '%s'", content)
	}
}

func TestDoStream_HTTPError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "bad key"}})

	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", []byte(`{}`))
	ch, err := proto.DoStream(context.Background(), req, 5*time.Second, NewChunkDecoder, DecodeError, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error == nil || !strings.Contains(chunk.Error.Error(), "bad key") {
		t.Fatalf("unexpected: %v", chunk.Error)
	}
}

func TestDoStream_NetworkError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		ReplyError(errors.New("dial timeout"))

	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", []byte(`{}`))
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
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		JSON(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "Hello from OpenAI"}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		})

	body, _ := BuildRequest("gpt-4", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "gpt-4", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
	if !chunk.Done || chunk.Content != "Hello from OpenAI" {
		t.Errorf("unexpected chunk: %+v", chunk)
	}
	if chunk.InputTokens != 10 || chunk.OutputTokens != 20 || chunk.TotalTokens != 30 {
		t.Errorf("unexpected tokens: %+v", chunk)
	}
}

func TestDoComplete_WithToolCalls(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		JSON(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content":           "",
				"reasoning_content": "thinking...",
				"tool_calls": []map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{
					"name": "get_weather", "arguments": `{"city":"SF"}`,
				}}},
			}}},
			"usage": map[string]any{
				"prompt_tokens":            5,
				"completion_tokens":        15,
				"prompt_tokens_details":    map[string]any{"cached_tokens": 3},
				"prompt_cache_hit_tokens":  2,
				"prompt_cache_miss_tokens": 8,
			},
		})

	body, _ := BuildRequest("gpt-4", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "gpt-4", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
	if chunk.Thinking != "thinking..." || len(chunk.ToolCalls) != 1 || chunk.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected: %+v", chunk)
	}
	if chunk.PromptCachedTokens != 3 || chunk.PromptCacheHitTokens != 2 || chunk.PromptCacheMissTokens != 8 {
		t.Errorf("unexpected cache tokens: %+v", chunk)
	}
}

func TestDoComplete_Error(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "Invalid API key"}})

	body, _ := BuildRequest("gpt-4", nil, llm.Config{Temperature: 1.0}, llm.LLMRequest{Model: "gpt-4", MaxTokens: 100})
	req := newHTTPRequest(t, "https://api.openai.com/v1/chat/completions", body)
	ch, err := proto.DoComplete(context.Background(), req, 30*time.Second, DecodeComplete, DecodeError)
	if err != nil {
		t.Fatal(err)
	}
	chunk := <-ch
	if chunk.Error == nil || !strings.Contains(chunk.Error.Error(), "Invalid API key") {
		t.Fatalf("unexpected: %v", chunk.Error)
	}
}

func TestDiscoverModels(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "gpt-4"}, {"id": "gpt-4o"}, {"id": "gpt-4o-mini"}}})

	cfg := llm.Config{Vendor: "openai", APIType: "openai", APIKey: "sk-test", BaseURL: "https://api.openai.com"}
	models, err := DiscoverModels(context.Background(), cfg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(models) != 3 || models[0].Name != "gpt-4" || models[0].Vendor != "openai" || models[0].APIType != "openai" {
		t.Errorf("unexpected: %+v", models)
	}
}

func TestDiscoverModels_HTTPError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Get("/v1/models").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "bad key"}})

	cfg := llm.Config{APIKey: "bad-key", BaseURL: "https://api.openai.com"}
	if _, err := DiscoverModels(context.Background(), cfg); err == nil {
		t.Fatal("expected error")
	}
}
