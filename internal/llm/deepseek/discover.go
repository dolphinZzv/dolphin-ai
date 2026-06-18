package deepseek

import (
	"context"
	"strings"

	"dolphin/internal/llm"
)

// DiscoverModels fetches the model list from DeepSeek's OpenAI-compatible
// /v1/models endpoint. For anthropic-compatible chat API, the base URL
// includes a /anthropic path suffix — strip it since the models endpoint
// is always at the root.
func DiscoverModels(ctx context.Context, cfg llm.Config) ([]llm.ModelConfig, error) {
	discoverCfg := cfg
	discoverCfg.BaseURL = strings.TrimSuffix(strings.TrimRight(discoverCfg.BaseURL, "/"), "/anthropic")
	return llm.DiscoverOpenAIModels(ctx, discoverCfg)
}
