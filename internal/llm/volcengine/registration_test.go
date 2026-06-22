package volcengine

import (
	"testing"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func TestRegistrationAnthropic(t *testing.T) {
	p := llm.NewProvider(llm.Config{Vendor: "volcengine", APIType: "anthropic", Model: "test-model"}, zap.NewNop())
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "volcengine" {
		t.Errorf("expected Name 'volcengine', got %q", p.Name())
	}
}

func TestRegistrationOpenAI(t *testing.T) {
	p := llm.NewProvider(llm.Config{Vendor: "volcengine", APIType: "openai", Model: "test-model", APIKey: "key"}, zap.NewNop())
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "volcengine" {
		t.Errorf("expected Name 'volcengine', got %q", p.Name())
	}
}
