package custom

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dolphin/internal/llm"
	"dolphin/internal/types"
	"github.com/h2non/gock"
	"go.uber.org/zap"
)

func TestAnthropicProvider_Name(t *testing.T) {
	p := &anthropicProvider{}
	if p.Name() != "anthropic" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestAnthropicProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: llm.Config{
				Models: []llm.ModelConfig{
					{Name: "claude-sonnet-4", Model: "claude-sonnet-4", Provider: "anthropic"},
				},
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "claude-sonnet-4" {
			t.Errorf("got %+v", models)
		}
	})

	t.Run("returns default model list when cfg.Models is empty", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: llm.Config{
				Model:       "claude-sonnet-4-6",
				MaxTokens:   8192,
				Temperature: 0.7,
			},
		}
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models returned error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 default model, got %d", len(models))
		}
		if models[0].Name != "claude-sonnet-4-6" {
			t.Errorf("Name = %q", models[0].Name)
		}
		if models[0].Provider != "anthropic" {
			t.Errorf("Provider = %q", models[0].Provider)
		}
		if models[0].MaxTokens != 8192 {
			t.Errorf("MaxTokens = %d", models[0].MaxTokens)
		}
		if models[0].Temperature != 0.7 {
			t.Errorf("Temperature = %f", models[0].Temperature)
		}
	})
}

func TestAnthropicProvider_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.anthropic.com").
		Post("/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	provider := &anthropicProvider{
		cfg:    llm.Config{Model: "claude-3-opus", APIKey: "ant-key"},
		logger: zap.NewNop(),
	}

	ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
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
		cfg:    llm.Config{Model: "claude-3", APIKey: "key"},
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
		cfg:    llm.Config{Model: "claude-3", APIKey: "key"},
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
		cfg:    llm.Config{Model: "claude-3", APIKey: "key", BaseURL: "https://custom.anthropic.com"},
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

	if content != "hi" {
		t.Fatalf("expected 'hi', got '%s'", content)
	}
}
