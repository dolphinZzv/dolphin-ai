package volcengine

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

func TestVolcengineAnthropicInit(t *testing.T) {
	p := llm.NewProvider(llm.Config{Vendor: "volcengine", APIType: "anthropic", Model: "test", BaseURL: "https://test.com"}, zap.NewNop())
	if p.Name() != "volcengine" {
		t.Errorf("expected volcengine, got %s", p.Name())
	}
}

func TestVolcengineAnthropicProvider_Name(t *testing.T) {
	p := &anthropicProvider{}
	if p.Name() != "volcengine" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestVolcengineAnthropicProvider_Models(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: llm.Config{
				Models: []llm.ModelConfig{
					{Name: "claude-3", Model: "claude-3", Provider: "volcengine"},
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
			cfg: llm.Config{
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

func TestVolcengineAnthropic_chatURL(t *testing.T) {
	t.Run("default base URL", func(t *testing.T) {
		p := &anthropicProvider{}
		url := p.chatURL("")
		if url != "https://ark.cn-beijing.volces.com/api/v3/v1/messages" {
			t.Errorf("unexpected URL: %s", url)
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		p := &anthropicProvider{}
		url := p.chatURL("https://custom.volcengine.com")
		if url != "https://custom.volcengine.com/v1/messages" {
			t.Errorf("unexpected URL: %s", url)
		}
	})
}

func TestVolcengineAnthropic_CompleteStream(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/v1/messages").
		Reply(200).
		BodyString("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n")

	provider := &anthropicProvider{
		cfg:    llm.Config{Model: "claude-3", APIKey: "vk-key"},
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

func TestVolcengineAnthropic_CompleteStreamHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/v1/messages").
		Reply(400).
		JSON(map[string]any{"error": map[string]any{"message": "bad"}})

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
	if !strings.Contains(chunk.Error.Error(), "bad") {
		t.Fatalf("unexpected error: %v", chunk.Error)
	}
}

func TestVolcengineAnthropic_CompleteStreamNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("https://ark.cn-beijing.volces.com").
		Post("/api/v3/v1/messages").
		ReplyError(errors.New("refused"))

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
}
