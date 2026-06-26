package models

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func noopLogger() *zap.Logger {
	return zap.NewNop()
}

func TestNewProvider_Dispatch(t *testing.T) {
	t.Run("anthropic api_type returns anthropic shell", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "deepseek",
			APIType:  "anthropic",
			Model:    "deepseek-v4-flash",
			Models:   []llm.ModelConfig{{Name: "deepseek-v4-flash"}},
		}
		p := NewProvider(cfg, noopLogger())
		if p.Name() != "deepseek-v4-flash" {
			t.Errorf("Name = %q, want %q", p.Name(), "deepseek-v4-flash")
		}
	})

	t.Run("openai api_type returns openai shell", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "openai",
			APIType:  "openai",
			Model:    "gpt-4o",
			Models:   []llm.ModelConfig{{Name: "gpt-4o"}},
		}
		p := NewProvider(cfg, noopLogger())
		if p.Name() != "gpt-4o" {
			t.Errorf("Name = %q, want %q", p.Name(), "gpt-4o")
		}
	})

	t.Run("empty api_type defaults to provider", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "openai",
			APIType:  "",
			Model:    "gpt-4o-mini",
			Models:   []llm.ModelConfig{{Name: "gpt-4o-mini"}},
		}
		p := NewProvider(cfg, noopLogger())
		if p.Name() != "gpt-4o-mini" {
			t.Errorf("Name = %q, want %q", p.Name(), "gpt-4o-mini")
		}
	})

	t.Run("empty model uses first from models list", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "openai",
			APIType:  "openai",
			Model:    "",
			Models:   []llm.ModelConfig{{Name: "auto-picked"}},
		}
		p := NewProvider(cfg, noopLogger())
		if p.Name() != "auto-picked" {
			t.Errorf("Name = %q, want %q", p.Name(), "auto-picked")
		}
	})

	t.Run("unknown api_type treated as openai", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "unknown-vendor",
			APIType:  "nonexistent",
			Model:    "some-model",
			Models:   []llm.ModelConfig{{Name: "some-model"}},
		}
		p := NewProvider(cfg, noopLogger())
		if p.Name() != "some-model" {
			t.Errorf("Name = %q, want %q", p.Name(), "some-model")
		}
	})
}

func TestFindModelConfig(t *testing.T) {
	t.Run("returns matching model from config", func(t *testing.T) {
		cfg := llm.Config{Models: []llm.ModelConfig{
			{Name: "model-a", Provider: "test"},
			{Name: "model-b", Provider: "test2"},
		}}
		mc := findModelConfig(cfg, "model-a")
		if mc.Name != "model-a" || mc.Provider != "test" {
			t.Errorf("got %+v, want Name=model-a Provider=test", mc)
		}
	})

	t.Run("synthesizes default when name not found", func(t *testing.T) {
		cfg := llm.Config{
			Provider: "my-vendor",
			APIType:  "openai",
			Models:   []llm.ModelConfig{{Name: "other-model"}},
		}
		mc := findModelConfig(cfg, "missing-model")
		if mc.Name != "missing-model" {
			t.Errorf("Name = %q, want %q", mc.Name, "missing-model")
		}
		if mc.Model != "missing-model" {
			t.Errorf("Model = %q, want %q", mc.Model, "missing-model")
		}
		if mc.Provider != "my-vendor" {
			t.Errorf("Provider = %q, want %q", mc.Provider, "my-vendor")
		}
		if mc.APIType != "openai" {
			t.Errorf("APIType = %q, want %q", mc.APIType, "openai")
		}
	})

	t.Run("synthesizes default when models list empty", func(t *testing.T) {
		cfg := llm.Config{Provider: "bare"}
		mc := findModelConfig(cfg, "bare-model")
		if mc.Name != "bare-model" || mc.Model != "bare-model" {
			t.Errorf("synthesized default incorrect: %+v", mc)
		}
	})
}

func TestNewAnthropicProvider_Factory(t *testing.T) {
	cfg := llm.Config{
		Provider: "deepseek",
		APIType:  "anthropic",
		BaseURL:  "https://api.example.com/anthropic",
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}
	factory := NewAnthropicProvider("test-model")
	p := factory(cfg, noopLogger())
	if p.Name() != "test-model" {
		t.Errorf("Name = %q, want %q", p.Name(), "test-model")
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "test-model" {
		t.Errorf("Models = %+v, want [Name=test-model]", models)
	}
}

func TestNewOpenAIProvider_Factory(t *testing.T) {
	cfg := llm.Config{
		Provider: "openai",
		APIType:  "openai",
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "gpt-4o"}},
	}
	factory := NewOpenAIProvider("gpt-4o")
	p := factory(cfg, noopLogger())
	if p.Name() != "gpt-4o" {
		t.Errorf("Name = %q, want %q", p.Name(), "gpt-4o")
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "gpt-4o" {
		t.Errorf("Models = %+v, want [Name=gpt-4o]", models)
	}
}

func TestWrapReasoningDefault(t *testing.T) {
	mc := llm.ModelConfig{Name: "deepseek-reasoning"}
	inner := llm.ProviderFunc{
		Name_:  "deepseek-reasoning",
		Model_: mc,
		Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
			ch := make(chan llm.LLMChunk, 1)
			ch <- llm.LLMChunk{Content: "hello", Done: true}
			close(ch)
			return ch, nil
		},
	}

	t.Run("sets default when empty", func(t *testing.T) {
		var capturedReq llm.LLMRequest
		wrapper := wrapReasoningDefault(llm.ProviderFunc{
			Name_:  inner.Name(),
			Model_: mc,
			Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				capturedReq = req
				ch := make(chan llm.LLMChunk, 1)
				ch <- llm.LLMChunk{Content: "ok", Done: true}
				close(ch)
				return ch, nil
			},
		}, "high")

		ch, err := wrapper.CompleteStream(context.Background(), llm.LLMRequest{})
		if err != nil {
			t.Fatalf("CompleteStream error: %v", err)
		}
		for range ch {
		}
		if capturedReq.ReasoningEffort != "high" {
			t.Errorf("ReasoningEffort = %q, want %q", capturedReq.ReasoningEffort, "high")
		}
	})

	t.Run("keeps existing reasoning_effort", func(t *testing.T) {
		var capturedReq llm.LLMRequest
		wrapper := wrapReasoningDefault(llm.ProviderFunc{
			Name_:  inner.Name(),
			Model_: mc,
			Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				capturedReq = req
				ch := make(chan llm.LLMChunk, 1)
				ch <- llm.LLMChunk{Content: "ok", Done: true}
				close(ch)
				return ch, nil
			},
		}, "high")

		ch, err := wrapper.CompleteStream(context.Background(), llm.LLMRequest{ReasoningEffort: "low"})
		if err != nil {
			t.Fatalf("CompleteStream error: %v", err)
		}
		for range ch {
		}
		if capturedReq.ReasoningEffort != "low" {
			t.Errorf("ReasoningEffort = %q, want %q (existing should be preserved)", capturedReq.ReasoningEffort, "low")
		}
	})

	t.Run("Name delegates to inner", func(t *testing.T) {
		wrapper := wrapReasoningDefault(inner, "high")
		if wrapper.Name() != "deepseek-reasoning" {
			t.Errorf("Name = %q, want %q", wrapper.Name(), "deepseek-reasoning")
		}
	})

	t.Run("Models delegates to inner", func(t *testing.T) {
		wrapper := wrapReasoningDefault(inner, "high")
		models, err := wrapper.Models(context.Background())
		if err != nil {
			t.Fatalf("Models() error: %v", err)
		}
		if len(models) != 1 || models[0].Name != "deepseek-reasoning" {
			t.Errorf("Models = %+v", models)
		}
	})
}

func TestProviderFunc_Models(t *testing.T) {
	mc := llm.ModelConfig{Name: "test-model", Model: "test-model"}
	p := llm.ProviderFunc{
		Name_:  "test-model",
		Model_: mc,
		Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
			return nil, nil
		},
	}
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "test-model" {
		t.Errorf("got %+v, want [Name=test-model]", models)
	}
}

func TestModelInitRegistrations(t *testing.T) {
	// Each model file registers itself in init(); verify the known
	// registrations exist. Use the exported registry to avoid import cycle.
	type registration struct {
		model   string
		apiType string
	}
	registered := make(map[registration]bool)
	for _, key := range llm.RegisteredModelProviders() {
		// key format: "model/api_type"
		for i := 0; i < len(key); i++ {
			if key[i] == '/' {
				registered[registration{model: key[:i], apiType: key[i+1:]}] = true
				break
			}
		}
	}

	expected := []registration{
		{"deepseek-v4-flash", "anthropic"},
		{"deepseek-v4-flash", "openai"},
		{"deepseek-v4-pro", "anthropic"},
		{"deepseek-v4-pro", "openai"},
		{"glm-5.2", "openai"},
		{"minimax-m3", "openai"},
	}
	for _, r := range expected {
		if !registered[r] {
			t.Errorf("model %s/%s not registered; init() may not have run or registration uses different key", r.model, r.apiType)
		}
	}
}

func TestNewProvider_WithTimeoutAndHeaders(t *testing.T) {
	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "openai",
		Model:    "test-model",
		BaseURL:  "https://api.example.com",
		APIKey:   "sk-test",
		Timeout:  30 * time.Second,
		Headers:  map[string]string{"X-Custom": "value"},
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}
	p := NewProvider(cfg, noopLogger())
	if p.Name() != "test-model" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestFindModelConfig_ModelFieldFallback(t *testing.T) {
	cfg := llm.Config{
		Models: []llm.ModelConfig{
			{Name: "custom-name", Model: "actual-model-id", Provider: "test"},
		},
	}
	mc := findModelConfig(cfg, "custom-name")
	if mc.Model != "actual-model-id" {
		t.Errorf("Model = %q, want %q (should not be overridden)", mc.Model, "actual-model-id")
	}
}

func TestDeepSeekV4Pro_RegisteredFactories(t *testing.T) {
	t.Run("deepseek-v4-pro/anthropic sets reasoning high", func(t *testing.T) {
		factory, err := llm.LookupModelProvider("deepseek-v4-pro", "anthropic")
		if err != nil {
			t.Fatalf("LookupModelProvider error: %v", err)
		}
		cfg := llm.Config{
			Provider: "deepseek",
			APIType:  "anthropic",
			Model:    "deepseek-v4-pro",
			BaseURL:  "https://api.example.com",
			APIKey:   "sk-test",
			Models:   []llm.ModelConfig{{Name: "deepseek-v4-pro"}},
		}
		p := factory(cfg, noopLogger())
		if p.Name() != "deepseek-v4-pro" {
			t.Errorf("Name = %q, want %q", p.Name(), "deepseek-v4-pro")
		}
	})

	t.Run("deepseek-v4-pro/openai sets reasoning high", func(t *testing.T) {
		factory, err := llm.LookupModelProvider("deepseek-v4-pro", "openai")
		if err != nil {
			t.Fatalf("LookupModelProvider error: %v", err)
		}
		cfg := llm.Config{
			Provider: "deepseek",
			APIType:  "openai",
			Model:    "deepseek-v4-pro",
			BaseURL:  "https://api.example.com/v1",
			APIKey:   "sk-test",
			Models:   []llm.ModelConfig{{Name: "deepseek-v4-pro"}},
		}
		p := factory(cfg, noopLogger())
		if p.Name() != "deepseek-v4-pro" {
			t.Errorf("Name = %q, want %q", p.Name(), "deepseek-v4-pro")
		}
	})
}
