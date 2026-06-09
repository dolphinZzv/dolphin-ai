package llm

import (
	"context"
	"testing"
)

func TestOpenAIProviderModels(t *testing.T) {
	t.Run("returns cfg.Models when populated", func(t *testing.T) {
		p := &openAIProvider{
			cfg: Config{
				Models: []ModelConfig{
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
			cfg: Config{
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

func TestOpenAIProviderName(t *testing.T) {
	p := &openAIProvider{}
	if p.Name() != "openai" {
		t.Errorf("Name = %q", p.Name())
	}
}
