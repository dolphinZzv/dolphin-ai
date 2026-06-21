package setup

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	_ "dolphin/internal/llm/custom"
	"dolphin/internal/llm/deepseek"
	_ "dolphin/internal/llm/models"
	_ "dolphin/internal/llm/volcengine"
)

type LLMBootstrapper struct{}

func (b *LLMBootstrapper) Name() string { return "llm" }
func (b *LLMBootstrapper) Index() int   { return 50 }
func (b *LLMBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.LLMProvider != nil {
		return nil
	}

	mgr := llm.NewManager()
	providerNames := discoverProviderNames(c.Config)
	if len(providerNames) == 0 {
		// Legacy single-provider mode.
		c.Logger.Warn("no providers configured via llm.<name>.api_key, falling back to legacy")
		provider := c.createProvider(ctx, "openai", nil)
		mgr.AddProvider("openai", provider)
	} else {
		for _, name := range providerNames {
			models := parseProviderModels(c.Config, name)
			c.Logger.Info("discovered provider",
				zap.String("name", name),
				zap.Int("models", len(models)),
			)
			provider := c.createProvider(ctx, name, models)
			mgr.AddProvider(name, provider)
		}
	}

	active := c.Config.GetString("llm.use")
	if active != "" {
		if err := mgr.SetActiveModel(active); err != nil {
			c.Logger.Warn("llm.use model not found, leaving default",
				zap.String("model", active), zap.Error(err))
		}
	}
	c.LLMProvider = mgr
	return nil
}

// discoverProviderNames finds all provider section names from config.
// It looks for keys matching llm.<name>.api_key, skipping known LLM-level fields.
func discoverProviderNames(cfg interface {
	GetString(string) string
	Keys() []string
},
) []string {
	seen := make(map[string]bool)
	var providers []string

	for _, key := range cfg.Keys() {
		// Match llm.<name>.api_key
		before, ok := strings.CutSuffix(key, ".api_key")
		if !ok {
			continue
		}
		name, ok := strings.CutPrefix(before, "llm.")
		if !ok || name == "" || strings.Contains(name, ".") {
			continue
		}
		if !seen[name] {
			providers = append(providers, name)
			seen[name] = true
		}
	}

	if len(providers) == 0 {
		return nil
	}

	return providers
}

func hasKey(keys []string, target string) bool {
	for _, k := range keys {
		if k == target {
			return true
		}
	}
	return false
}

func parseProviderModels(cfg interface {
	GetString(string) string
	GetInt(string) int
	GetFloat(string) float64
	GetDuration(string) time.Duration
	GetBool(string) bool
	Keys() []string
}, provider string,
) []llm.ModelConfig {
	var models []llm.ModelConfig
	for i := 0; ; i++ {
		prefix := "llm." + provider + ".models." + strconv.Itoa(i)
		name := cfg.GetString(prefix + ".name")
		if name == "" {
			break
		}
		vendor := cfg.GetString("llm." + provider + ".provider")
		apiType := cfg.GetString("llm." + provider + ".api_type")

		maxTokens := cfg.GetInt(prefix + ".max_tokens")
		if maxTokens == 0 {
			maxTokens = cfg.GetInt("llm.max_tokens")
		}
		if maxTokens == 0 {
			maxTokens = 4096
		}

		maxRetries := cfg.GetInt(prefix + ".max_retries")
		if maxRetries == 0 {
			maxRetries = cfg.GetInt("llm.max_retries")
		}

		timeout := cfg.GetDuration(prefix + ".timeout")
		if timeout == 0 {
			timeout = cfg.GetDuration("llm.timeout")
		}

		maxConcurrency := cfg.GetInt(prefix + ".limit.max_concurrency")

		stream := true
		streamSet := false
		if hasKey(cfg.Keys(), prefix+".stream") {
			stream = cfg.GetBool(prefix + ".stream")
			streamSet = true
		}

		models = append(models, llm.ModelConfig{
			Name:            name,
			Provider:        provider,
			Vendor:          vendor,
			APIType:         apiType,
			Model:           name,
			MaxTokens:       maxTokens,
			Temperature:     cfg.GetFloat(prefix + ".temperature"),
			TopP:            cfg.GetFloat(prefix + ".top_p"),
			MaxRetries:      maxRetries,
			MaxConcurrency:  maxConcurrency,
			Timeout:         timeout,
			ReasoningEffort: cfg.GetString(prefix + ".reasoning_effort"),
			Thinking:        cfg.GetBool(prefix + ".thinking"),
			Stream:          stream,
			StreamSet:       streamSet,
			Disabled:        cfg.GetBool(prefix + ".disabled"),
		})
	}
	return models
}

func (c *Context) createProvider(ctx context.Context, name string, models []llm.ModelConfig) llm.Provider {
	modelDiscover := c.Config.GetBool("llm." + name + ".model_discover")

	cfg := llm.Config{
		Provider:      name,
		Vendor:        c.Config.GetString("llm." + name + ".provider"),
		APIType:       c.Config.GetString("llm." + name + ".api_type"),
		APIKey:        c.Config.GetString("llm." + name + ".api_key"),
		BaseURL:       c.Config.GetString("llm." + name + ".base_url"),
		MaxTokens:     c.Config.GetInt("llm.max_tokens"),
		MaxRetries:    c.Config.GetInt("llm.max_retries"),
		Timeout:       c.Config.GetDuration("llm.timeout"),
		Headers:       c.Config.GetStringMap("llm." + name + ".headers"),
		ModelDiscover: modelDiscover,
	}
	if len(models) > 0 {
		cfg.Models = models
	} else if modelDiscover {
		discovered, err := discoverProviderModels(ctx, cfg)
		if err != nil {
			c.Logger.Warn("model discovery failed",
				zap.String("provider", name),
				zap.Error(err),
			)
		} else if len(discovered) > 0 {
			c.Logger.Info("model discovery succeeded",
				zap.String("provider", name),
				zap.Int("count", len(discovered)),
			)
			cfg.Models = discovered
		}
	}
	return llm.NewProvider(cfg, c.Logger)
}

// discoverProviderModels dispatches model discovery to the vendor-specific
// implementation, falling back to generic api_type-based discovery.
func discoverProviderModels(ctx context.Context, cfg llm.Config) ([]llm.ModelConfig, error) {
	switch cfg.Vendor {
	case "deepseek":
		return deepseek.DiscoverModels(ctx, cfg)
	default:
		return llm.DiscoverModels(ctx, cfg, nil)
	}
}
