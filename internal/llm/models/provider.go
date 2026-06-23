package models

import (
	"strings"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

// NewProvider constructs a generic protocol provider for cfg, dispatching by
// api_type to the OpenAI or Anthropic shell. Unlike the per-model factories
// registered via RegisterModelProvider, this does not require a pre-registered
// provider file — it builds a generic shell on demand. Intended for tests and
// dynamic model lists where a model has no known quirks. There is no fallback:
// api_type "anthropic" yields an Anthropic shell, everything else OpenAI.
func NewProvider(cfg llm.Config, log *zap.Logger) llm.Provider {
	apiType := cfg.APIType
	if apiType == "" {
		apiType = cfg.Provider
	}
	name := cfg.Model
	if name == "" && len(cfg.Models) > 0 {
		name = cfg.Models[0].Name
	}
	switch strings.ToLower(apiType) {
	case "anthropic":
		return NewAnthropicProvider(name)(cfg, log)
	default:
		return NewOpenAIProvider(name)(cfg, log)
	}
}
