package llm

import (
	"context"
	"testing"
)

func TestAnthropicProviderModels(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &anthropicProvider{
			cfg: Config{
				Models: []ModelConfig{
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
			cfg: Config{
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

func TestAnthropicProviderName(t *testing.T) {
	p := &anthropicProvider{}
	if p.Name() != "anthropic" {
		t.Errorf("Name = %q", p.Name())
	}
}
