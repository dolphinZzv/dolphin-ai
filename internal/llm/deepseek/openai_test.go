package deepseek

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

func TestDeepSeekOpenAIInit(t *testing.T) {
	p := llm.NewProvider(llm.Config{Vendor: "deepseek", APIType: "openai", Model: "test"}, zap.NewNop())
	if p.Name() != "deepseek" {
		t.Errorf("expected deepseek, got %s", p.Name())
	}
}

func TestDeepSeekOpenAIProvider_Name(t *testing.T) {
	p := &openAIProvider{}
	if p.Name() != "deepseek" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestDeepSeekOpenAIProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Models: []llm.ModelConfig{
					{Name: "deepseek-chat", Model: "deepseek-chat", Provider: "deepseek"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "deepseek-chat" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model when cfg.Models is empty", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Model:       "deepseek-chat",
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
		if models[0].Name != "deepseek-chat" {
			t.Errorf("Name = %q", models[0].Name)
		}
	})
}

func TestDeepSeekOpenAI_chatURL(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("")
		if url != "https://api.deepseek.com/v1/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p := &openAIProvider{}
		url := p.chatURL("https://custom.deepseek.com")
		if url != "https://custom.deepseek.com/v1/chat/completions" {
			t.Errorf("unexpected URL: %s", url)
		}
	})
}

func TestDeepSeekOpenAI_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "deepseek-chat", APIKey: "ds-key", BaseURL: "https://api.deepseek.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
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

func TestDeepSeekOpenAI_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "Invalid API key"}})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "deepseek-chat", APIKey: "bad-key", BaseURL: "https://api.deepseek.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
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

func TestDeepSeekOpenAI_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Post("/v1/chat/completions").
		ReplyError(errors.New("connection timeout"))

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "deepseek-chat", APIKey: "key", BaseURL: "https://api.deepseek.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
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

func TestDeepSeekOpenAI_CompleteStreamEmptyResponse(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "deepseek-chat", APIKey: "key", BaseURL: "https://api.deepseek.com"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if chunk := <-ch; chunk.Error != nil {
		t.Fatal(chunk.Error)
	}
}
