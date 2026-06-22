package custom

import (
	"testing"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func TestRegistrationAnthropic(t *testing.T) {
	p := llm.NewProvider(llm.Config{Provider: "custom", APIType: "anthropic", APIKey: "key", Model: "claude-3"}, zap.NewNop())
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "anthropic" {
		t.Errorf("expected Name 'anthropic', got %q", p.Name())
	}
}

func TestRegistrationOpenAI(t *testing.T) {
	p := llm.NewProvider(llm.Config{Provider: "custom", APIType: "openai", APIKey: "key", Model: "gpt-4"}, zap.NewNop())
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "openai" {
		t.Errorf("expected Name 'openai', got %q", p.Name())
	}
}
