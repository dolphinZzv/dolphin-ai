package deepseek

import (
	"context"
	"testing"

	"github.com/h2non/gock"

	"dolphin/internal/llm"
)

func TestDeepSeekDiscoverModels(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{
			"data": []map[string]any{
				{"id": "deepseek-chat"},
				{"id": "deepseek-reasoner"},
			},
		})

	cfg := llm.Config{
		Vendor:  "deepseek",
		APIType: "anthropic",
		APIKey:  "sk-test",
		BaseURL: "https://api.deepseek.com/anthropic",
	}
	models, err := DiscoverModels(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "deepseek-chat" {
		t.Errorf("unexpected model[0]: %+v", models[0])
	}
}

func TestDeepSeekDiscoverModels_stripsAnthropicPath(t *testing.T) {
	// Verify that /anthropic is stripped from the base URL before discovery.
	cfg := llm.Config{
		Vendor:  "deepseek",
		APIType: "anthropic",
		APIKey:  "sk-test",
		BaseURL: "https://api.deepseek.com/anthropic",
	}

	url := llm.OpenAIModelsURL(cfg.BaseURL)
	if url != "https://api.deepseek.com/anthropic/v1/models" {
		t.Fatalf("before stripping, URL should include /anthropic, got: %s", url)
	}

	// The DiscoverModels function should strip /anthropic and call OpenAI models.
	defer gock.Off()
	gock.New("https://api.deepseek.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "deepseek-chat"}}})

	models, err := DiscoverModels(context.Background(), cfg)
	if err != nil {
		t.Fatalf("DiscoverModels error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "deepseek-chat" {
		t.Errorf("unexpected models: %+v", models)
	}
}

func TestDeepSeekDiscoverModels_HTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("https://api.deepseek.com").
		Get("/v1/models").
		Reply(401).
		JSON(map[string]any{"error": map[string]any{"message": "unauthorized"}})

	cfg := llm.Config{
		Vendor:  "deepseek",
		APIKey:  "bad-key",
		BaseURL: "https://api.deepseek.com",
	}
	_, err := DiscoverModels(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}
