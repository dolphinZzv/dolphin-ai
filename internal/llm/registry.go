package llm

import "go.uber.org/zap"

// ProviderFactory creates a Provider from config and logger.
type ProviderFactory func(cfg Config, logger *zap.Logger) Provider

var providerFactories = map[string]ProviderFactory{
	"openai":    func(cfg Config, logger *zap.Logger) Provider { return &openAIProvider{cfg: cfg, logger: logger} },
	"anthropic": func(cfg Config, logger *zap.Logger) Provider { return &anthropicProvider{cfg: cfg, logger: logger} },
}

// RegisterProvider registers a provider factory under the given name.
// If called before NewProvider, it overrides the default factory for that name.
func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

func NewProvider(cfg Config, logger *zap.Logger) Provider {
	factory, ok := providerFactories[cfg.Provider]
	if !ok {
		logger.Warn("unknown LLM provider, falling back to openai", zap.String("provider", cfg.Provider))
		factory = providerFactories["openai"]
	}
	return factory(cfg, logger)
}
