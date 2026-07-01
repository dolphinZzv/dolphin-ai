package llm

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// ProviderFactory creates a Provider from config and logger.
type ProviderFactory func(cfg Config, logger *zap.Logger) Provider

// modelFactories maps "model/api_type" → factory. Each (model, api_type) pair
// is an independent provider; there is no fallback. A miss is a hard error,
// never a silent default to "openai".
var modelFactories = make(map[string]ProviderFactory)

// RegisterModelProvider registers a factory for the given "model/api_type"
// key (e.g. "minimax-m3/openai"). Called from init() in each per-model file.
func RegisterModelProvider(key string, factory ProviderFactory) {
	modelFactories[key] = factory
}

// UnregisterModelProvider removes a registered factory. Intended for test
// cleanup of dynamically registered providers.
func UnregisterModelProvider(key string) {
	delete(modelFactories, key)
}

// LookupModelProvider returns the factory for model/apiType, or an error if no
// per-model provider exists. There is no compatibility fallback: every model
// must have an explicit provider file.
func LookupModelProvider(model, apiType string) (ProviderFactory, error) {
	key := model + "/" + apiType
	if f, ok := modelFactories[key]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("llm: no provider registered for %q (model=%s api_type=%s); add a provider file under internal/llm/models", key, model, apiType)
}

// RegisteredModelProviders returns the sorted list of registered model/api_type
// keys, for diagnostics and tests.
func RegisteredModelProviders() []string {
	out := make([]string, 0, len(modelFactories))
	for k := range modelFactories {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// DiscoverModels fetches the model list from the API. Dispatch is by api_type
// only; per-model providers are not involved in discovery.
func DiscoverModels(ctx context.Context, cfg Config, logger *zap.Logger) ([]ModelConfig, error) {
	apiType := cfg.APIType
	if apiType == "" {
		apiType = cfg.Provider
	}
	apiType = strings.ToLower(apiType)

	switch apiType {
	case "anthropic":
		return discoverAnthropicModels(ctx, cfg)
	case "openai-responses":
		return discoverResponsesModels(ctx, cfg)
	default:
		return discoverOpenAIModels(ctx, cfg)
	}
}

// The proto/openai and proto/anthropic packages inject their discoverers at
// init time via SetOpenAIDiscoverer / SetAnthropicDiscoverer. This breaks what
// would otherwise be an import cycle (llm → proto → llm).
var (
	openAIDiscoverer    func(ctx context.Context, cfg Config) ([]ModelConfig, error)
	anthropicDiscoverer func(ctx context.Context, cfg Config) ([]ModelConfig, error)
	responsesDiscoverer func(ctx context.Context, cfg Config) ([]ModelConfig, error)
)

// SetOpenAIDiscoverer is called from proto/openai init.
func SetOpenAIDiscoverer(f func(ctx context.Context, cfg Config) ([]ModelConfig, error)) {
	openAIDiscoverer = f
}

// SetAnthropicDiscoverer is called from proto/anthropic init.
func SetAnthropicDiscoverer(f func(ctx context.Context, cfg Config) ([]ModelConfig, error)) {
	anthropicDiscoverer = f
}

// SetResponsesDiscoverer is called from proto/responses init.
func SetResponsesDiscoverer(f func(ctx context.Context, cfg Config) ([]ModelConfig, error)) {
	responsesDiscoverer = f
}

func discoverOpenAIModels(ctx context.Context, cfg Config) ([]ModelConfig, error) {
	if openAIDiscoverer == nil {
		return nil, fmt.Errorf("llm: openai discoverer not registered")
	}
	return openAIDiscoverer(ctx, cfg)
}

func discoverAnthropicModels(ctx context.Context, cfg Config) ([]ModelConfig, error) {
	if anthropicDiscoverer == nil {
		return nil, fmt.Errorf("llm: anthropic discoverer not registered")
	}
	return anthropicDiscoverer(ctx, cfg)
}

func discoverResponsesModels(ctx context.Context, cfg Config) ([]ModelConfig, error) {
	if responsesDiscoverer == nil {
		return nil, fmt.Errorf("llm: responses discoverer not registered")
	}
	return responsesDiscoverer(ctx, cfg)
}
