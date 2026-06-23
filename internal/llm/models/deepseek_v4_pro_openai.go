package models

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func init() {
	llm.RegisterModelProvider("deepseek-v4-pro/openai", newDeepSeekV4ProOpenAI)
}

// newDeepSeekV4ProOpenAI wraps the generic OpenAI shell to apply the
// model-specific default reasoning_effort (previously a global LLM hook).
func newDeepSeekV4ProOpenAI(cfg llm.Config, log *zap.Logger) llm.Provider {
	inner := NewOpenAIProvider("deepseek-v4-pro")(cfg, log)
	return wrapReasoningDefault(inner, "high")
}

// wrapReasoningDefault returns a Provider that sets req.ReasoningEffort to def
// when unset, then delegates to inner. Shared by the openai/anthropic variants
// of reasoning-capable models.
func wrapReasoningDefault(inner llm.Provider, def string) llm.Provider {
	return reasoningWrapper{inner: inner, def: def}
}

type reasoningWrapper struct {
	inner llm.Provider
	def   string
}

func (w reasoningWrapper) Name() string { return w.inner.Name() }
func (w reasoningWrapper) Models(ctx context.Context) ([]llm.ModelConfig, error) {
	return w.inner.Models(ctx)
}
func (w reasoningWrapper) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	if req.ReasoningEffort == "" {
		req.ReasoningEffort = w.def
	}
	return w.inner.CompleteStream(ctx, req)
}
