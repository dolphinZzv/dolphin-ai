package llm

import "go.uber.org/zap"

// ProviderFactory creates a Provider from config and logger.
type ProviderFactory func(cfg Config, logger *zap.Logger) Provider

var providerFactories = make(map[string]ProviderFactory)

// RegisterProvider registers a provider factory under the given name.
// If called before NewProvider, it overrides the default factory for that name.
func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

func NewProvider(cfg Config, logger *zap.Logger) Provider {
	apiType := cfg.APIType
	if apiType == "" {
		apiType = cfg.Provider
	}

	// Two-tier lookup: vendor/api_type → api_type → "openai"
	var factory ProviderFactory
	if cfg.Vendor != "" {
		factory = providerFactories[cfg.Vendor+"/"+apiType]
	}
	if factory == nil {
		factory = providerFactories[apiType]
	}
	if factory == nil {
		logger.Warn("unknown LLM API type, falling back to openai",
			zap.String("vendor", cfg.Vendor),
			zap.String("api_type", apiType),
		)
		factory = providerFactories["openai"]
	}
	return factory(cfg, logger)
}
