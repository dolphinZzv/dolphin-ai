package models

import (
	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func init() {
	llm.RegisterModelProvider("deepseek-v4-pro/anthropic", newDeepSeekV4ProAnthropic)
}

// newDeepSeekV4ProAnthropic wraps the generic Anthropic shell to apply the
// model-specific default reasoning_effort (previously a global LLM hook).
func newDeepSeekV4ProAnthropic(cfg llm.Config, log *zap.Logger) llm.Provider {
	inner := NewAnthropicProvider("deepseek-v4-pro")(cfg, log)
	return wrapReasoningDefault(inner, "high")
}
