package custom

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

func TestOpenAIProvider_Name(t *testing.T) {
	p := &openAIProvider{}
	if p.Name() != "openai" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestOpenAIProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Models: []llm.ModelConfig{
					{Name: "gpt-4o", Model: "gpt-4o", Provider: "openai"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "gpt-4o" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model list when cfg.Models is empty", func(t *testing.T) {
		p := &openAIProvider{
			cfg: llm.Config{
				Model:       "gpt-4o",
				MaxTokens:   4096,
				Temperature: 0.5,
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 default model, got %d", len(models))
		}
		if models[0].Name != "gpt-4o" {
			t.Errorf("Name = %q", models[0].Name)
		}
		if models[0].Provider != "openai" {
			t.Errorf("Provider = %q", models[0].Provider)
		}
		if models[0].MaxTokens != 4096 {
			t.Errorf("MaxTokens = %d", models[0].MaxTokens)
		}
		if models[0].Temperature != 0.5 {
			t.Errorf("Temperature = %f", models[0].Temperature)
		}
	})
}

func TestOpenAIProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(200).
		BodyString("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "gpt-4", APIKey: "test-key"},
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

func TestOpenAIProvider_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(401).
		JSON(map[string]any{
			"error": map[string]any{"message": "Invalid API key"},
		})

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "gpt-4", APIKey: "bad-key"},
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

func TestOpenAIProvider_CompleteStreamHTTPErrorNoMessage(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.openai.com").
		Post("/v1/chat/completions").
		Reply(500).
		BodyString("Internal Server Error")

	provider := &openAIProvider{
		cfg:    llm.Config{Model: "gpt-4", APIKey: "key"},
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
		cfg:    llm.Config{Model: "gpt-4", APIKey: "key"},
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
		cfg:    llm.Config{Model: "gpt-4", APIKey: "key", BaseURL: "https://custom.example.com"},
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
		cfg:    llm.Config{Model: "gpt-4", APIKey: "key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
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
