package llm

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestProviderFunc_Methods(t *testing.T) {
	p := ProviderFunc{
		Name_:  "test-provider",
		Model_: ModelConfig{Name: "test-model", Model: "test-model"},
		Stream_: func(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
			ch := make(chan LLMChunk, 1)
			ch <- LLMChunk{Content: "hello", Done: true}
			close(ch)
			return ch, nil
		},
	}
	if p.Name() != "test-provider" {
		t.Errorf("Name = %q", p.Name())
	}
	ch, err := p.CompleteStream(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	var got LLMChunk
	for c := range ch {
		got = c
	}
	if got.Content != "hello" || !got.Done {
		t.Errorf("chunk = %+v", got)
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 || models[0].Name != "test-model" {
		t.Errorf("models = %+v", models)
	}
}

func TestUnregisterModelProvider(t *testing.T) {
	key := "unregister-me/openai"
	RegisterModelProvider(key, func(cfg Config, logger *zap.Logger) Provider { return nil })
	if _, err := LookupModelProvider("unregister-me", "openai"); err != nil {
		t.Fatal("should be registered before unregister")
	}
	UnregisterModelProvider(key)
	if _, err := LookupModelProvider("unregister-me", "openai"); err == nil {
		t.Fatal("should be unregistered")
	}
	UnregisterModelProvider(key) // no-op, should not panic
}

func TestRegisteredModelProviders_Sorting(t *testing.T) {
	keys := []string{"z-model/anthropic", "a-model/openai", "m-model/openai"}
	for _, k := range keys {
		RegisterModelProvider(k, func(cfg Config, logger *zap.Logger) Provider {
			return nil
		})
	}
	defer func() {
		for _, k := range keys {
			delete(modelFactories, k)
		}
	}()

	got := RegisteredModelProviders()
	if len(got) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("not sorted: %q before %q", got[i-1], got[i])
		}
	}
}

func TestDiscoverModels_OpenAI(t *testing.T) {
	var called bool
	SetOpenAIDiscoverer(func(ctx context.Context, cfg Config) ([]ModelConfig, error) {
		called = true
		return []ModelConfig{{Name: "discovered-model"}}, nil
	})
	defer func() { openAIDiscoverer = nil }()

	models, err := DiscoverModels(context.Background(), Config{APIType: "openai", BaseURL: "https://api.example.com"}, nil)
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if !called {
		t.Fatal("OpenAI discoverer was not called")
	}
	if len(models) != 1 || models[0].Name != "discovered-model" {
		t.Errorf("models = %+v", models)
	}
}

func TestDiscoverModels_Anthropic(t *testing.T) {
	var called bool
	SetAnthropicDiscoverer(func(ctx context.Context, cfg Config) ([]ModelConfig, error) {
		called = true
		return []ModelConfig{{Name: "claude-model"}}, nil
	})
	defer func() { anthropicDiscoverer = nil }()

	models, err := DiscoverModels(context.Background(), Config{APIType: "anthropic"}, nil)
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if !called {
		t.Fatal("Anthropic discoverer was not called")
	}
	if len(models) != 1 || models[0].Name != "claude-model" {
		t.Errorf("models = %+v", models)
	}
}

func TestDiscoverModels_DefaultsToOpenAI(t *testing.T) {
	var called bool
	SetOpenAIDiscoverer(func(ctx context.Context, cfg Config) ([]ModelConfig, error) {
		called = true
		return nil, nil
	})
	defer func() { openAIDiscoverer = nil }()

	DiscoverModels(context.Background(), Config{APIType: "", Provider: "openai"}, nil)
	if !called {
		t.Fatal("empty api_type should default to openai")
	}
}

func TestDiscoverModels_UnknownAPI(t *testing.T) {
	_, err := DiscoverModels(context.Background(), Config{APIType: "nonexistent"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown api_type (no default discoverer)")
	}
}

func TestDiscoverModels_OpenAINilDiscoverer(t *testing.T) {
	openAIDiscoverer = nil
	_, err := discoverOpenAIModels(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error when openai discoverer is nil")
	}
}

func TestDiscoverModels_AnthropicNilDiscoverer(t *testing.T) {
	anthropicDiscoverer = nil
	_, err := discoverAnthropicModels(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error when anthropic discoverer is nil")
	}
}
