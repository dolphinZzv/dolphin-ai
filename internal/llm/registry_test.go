package llm

import (
	"testing"

	"go.uber.org/zap"
)

func TestRegisterProvider_Dynamic(t *testing.T) {
	// Register a custom provider at runtime.
	RegisterProvider("custom-provider", func(cfg Config, logger *zap.Logger) Provider {
		return &anthropicProvider{cfg: cfg, logger: logger}
	})
	defer delete(providerFactories, "custom-provider")

	p := NewProvider(Config{Provider: "custom-provider", APIKey: "key"}, zap.NewNop())
	if p.Name() != "anthropic" {
		t.Fatalf("expected 'anthropic', got '%s'", p.Name())
	}
}

func TestNewProvider_FallbackToOpenAI(t *testing.T) {
	p := NewProvider(Config{Provider: "nonexistent", APIKey: "key"}, zap.NewNop())
	if p.Name() != "openai" {
		t.Fatalf("expected fallback to 'openai', got '%s'", p.Name())
	}
}
