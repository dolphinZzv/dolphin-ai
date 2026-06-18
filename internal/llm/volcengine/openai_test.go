package volcengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/h2non/gock"
	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

func TestVolcengineOpenAIInit(t *testing.T) {
	p := llm.NewProvider(llm.Config{Vendor: "volcengine", APIType: "openai", Model: "test", BaseURL: "https://test.com"}, zap.NewNop())
	if p.Name() != "volcengine" {
		t.Errorf("expected volcengine, got %s", p.Name())
	}
}

func TestVolcengineOpenAIProvider_Name(t *testing.T) {
	p := &openAIProvider{}
	if p.Name() != "volcengine" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestVolcengineOpenAIProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Models: []llm.ModelConfig{
					{Name: "ark-model", Model: "ark-model", Provider: "volcengine"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "ark-model" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model when cfg.Models is empty", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Model:       "ark-model",
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
		if models[0].Name != "ark-model" {
			t.Errorf("Name = %q", models[0].Name)
		}
	})
}

func TestVolcengineOpenAI_chatURL(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("")
		if url != "https://ark.cn-beijing.volces.com/api/v3/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("https://custom.volcengine.com")
		if url != "https://custom.volcengine.com/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})
}

func TestVolcengineOpenAI_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "vk-key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
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

func TestVolcengineOpenAI_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "auth failed"}})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "bad-key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "auth failed") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestVolcengineOpenAI_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		ReplyError(errors.New("timeout"))

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
}

func TestVolcengineOpenAI_CompleteStreamEmptyResponse(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(200).
		BodyString("")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if chunk := <-ch; chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
}

func TestVolcengineOpenAI_CompleteStream_NonStreaming(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(200).
		JSON(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "non-streaming response",
					},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "vk-key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	var inputTokens, outputTokens, totalTokens int
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		content += chunk.Content
		if chunk.InputTokens > 0 {
			inputTokens = chunk.InputTokens
		}
		if chunk.OutputTokens > 0 {
			outputTokens = chunk.OutputTokens
		}
		if chunk.TotalTokens > 0 {
			totalTokens = chunk.TotalTokens
		}
		if !chunk.Done {
			t.Error("expected single chunk with Done=true")
		}
	}
	if content != "non-streaming response" {
		t.Fatalf("expected 'non-streaming response', got '%s'", content)
	}
	if inputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", inputTokens)
	}
	if outputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", outputTokens)
	}
	if totalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", totalTokens)
	}
}

func TestVolcengineOpenAI_CompleteStream_NonStreaming_ToolCalls(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(200).
		JSON(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call-1",
								"type": "function",
								"function": map[string]any{
									"name":      "greet",
									"arguments": `{"name":"world"}`,
								},
							},
						},
					},
				},
			},
		})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "vk-key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatal(err)
	}

	var toolCalls []types.ToolCall
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", toolCalls[0].Name)
	}
	if toolCalls[0].ID != "call-1" {
		t.Fatalf("expected 'call-1', got '%s'", toolCalls[0].ID)
	}
	if toolCalls[0].Arguments != `{"name":"world"}` {
		t.Fatalf("expected arguments '%s', got '%s'", `{"name":"world"}`, toolCalls[0].Arguments)
	}
}

func TestVolcengineOpenAI_CompleteStream_NonStreaming_HTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "model not found"}})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "bad-model", APIKey: "key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
	if !strings.Contains(chunk.Error.Error(), "model not found") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestVolcengineOpenAI_CompleteStream_NonStreaming_NetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		ReplyError(errors.New("connection refused"))

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk")
	}
}

func TestVolcengineOpenAI_CompleteStream_NonStreaming_InvalidJSON(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/chat/completions").
		Reply(200).
		BodyString("not json")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "ark-model", APIKey: "key", BaseURL: "https://ark.cn-beijing.volces.com/api/v3"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Error == nil {
		t.Fatal("expected error chunk for invalid JSON")
	}
}
