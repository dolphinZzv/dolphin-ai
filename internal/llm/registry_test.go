package llm

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

type stubProvider struct {
	name string
}

func (s *stubProvider) Name() string                           { return s.name }
func (s *stubProvider) Models(_ context.Context) ([]ModelConfig, error) { return nil, nil }
func (s *stubProvider) CompleteStream(_ context.Context, _ LLMRequest) (<-chan LLMChunk, error) {
	return nil, nil
}

func TestRegisterProvider_Dynamic(t *testing.T) {
	RegisterProvider("custom-provider", func(cfg Config, logger *zap.Logger) Provider {
		return &stubProvider{name: "custom"}
	})
	defer delete(providerFactories, "custom-provider")

	p := NewProvider(Config{Provider: "custom-provider", APIKey: "key"}, zap.NewNop())
	if p.Name() != "custom" {
		t.Fatalf("expected 'custom', got '%s'", p.Name())
	}
}

func TestNewProvider_FallbackToOpenAI(t *testing.T) {
	RegisterProvider("openai", func(cfg Config, logger *zap.Logger) Provider {
		return &stubProvider{name: "openai"}
	})
	defer delete(providerFactories, "openai")

	p := NewProvider(Config{Provider: "nonexistent", APIKey: "key"}, zap.NewNop())
	if p.Name() != "openai" {
		t.Fatalf("expected fallback to 'openai', got '%s'", p.Name())
	}
}
