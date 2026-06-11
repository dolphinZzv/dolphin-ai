package setup

import (
	"context"
	"strconv"
	"strings"
	"time"

	"dolphin/internal/llm"
	_ "dolphin/internal/llm/custom"
	_ "dolphin/internal/llm/deepseek"
	_ "dolphin/internal/llm/volcengine"

	"go.uber.org/zap"
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
		provider := c.createProvider("openai", nil)
		mgr.AddProvider("openai", provider)
	} else {
		for _, name := range providerNames {
			models := parseProviderModels(c.Config, name)
			c.Logger.Info("discovered provider",
				zap.String("name", name),
				zap.Int("models", len(models)),
			)
			provider := c.createProvider(name, models)
			mgr.AddProvider(name, provider)
		}
	}

	active := c.Config.GetString("llm.use")
	if active != "" {
		mgr.SetActiveModel(active)
	}
	c.LLMProvider = mgr
	return nil
}

// discoverProviderNames finds all provider section names from config.
// It looks for keys matching llm.<name>.api_key, skipping known LLM-level fields.
func discoverProviderNames(cfg interface {
	GetString(string) string
	Keys() []string
}) []string {
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

func parseProviderModels(cfg interface {
	GetString(string) string
	GetInt(string) int
	GetFloat(string) float64
	GetDuration(string) time.Duration
}, provider string) []llm.ModelConfig {
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

		maxRetries := cfg.GetInt(prefix + ".max_retries")
		if maxRetries == 0 {
			maxRetries = cfg.GetInt("llm.max_retries")
		}

		timeout := cfg.GetDuration(prefix + ".timeout")
		if timeout == 0 {
			timeout = cfg.GetDuration("llm.timeout")
		}

		models = append(models, llm.ModelConfig{
			Name:            name,
			Provider:        provider,
			Vendor:          vendor,
			APIType:         apiType,
			Model:           name,
			MaxTokens:       maxTokens,
			Temperature:     cfg.GetFloat(prefix + ".temperature"),
			MaxRetries:      maxRetries,
			Timeout:         timeout,
			ReasoningEffort: cfg.GetString(prefix + ".reasoning_effort"),
		})
	}
	return models
}

func (c *Context) createProvider(name string, models []llm.ModelConfig) llm.Provider {
	cfg := llm.Config{
		Provider:   name,
		Vendor:     c.Config.GetString("llm." + name + ".provider"),
		APIType:    c.Config.GetString("llm." + name + ".api_type"),
		APIKey:     c.Config.GetString("llm." + name + ".api_key"),
		BaseURL:    c.Config.GetString("llm." + name + ".base_url"),
		MaxTokens:  c.Config.GetInt("llm.max_tokens"),
		MaxRetries: c.Config.GetInt("llm.max_retries"),
		Timeout:    c.Config.GetDuration("llm.timeout"),
		Headers:    c.Config.GetStringMap("llm." + name + ".headers"),
	}
	if len(models) > 0 {
		cfg.Models = models
	}
	return llm.NewProvider(cfg, c.Logger)
}
