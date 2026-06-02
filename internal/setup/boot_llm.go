package setup

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dolphin/internal/llm"

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
		providerName := c.Config.GetString("llm.provider")
		if providerName == "" {
			providerName = "openai"
		}
		c.Logger.Warn("no providers configured via llm.<name>.api_key, falling back to legacy",
			zap.String("provider", providerName))
		provider := c.createProvider(providerName, nil)
		mgr.AddProvider(providerName, provider)
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

	active := c.Config.GetString("llm.model")
	if active != "" {
		mgr.SetActiveModel(active)
	}
	c.LLMProvider = mgr
	return nil
}

// discoverProviderNames finds all provider section names from config.
// It looks for keys matching llm.<name>.api_key, skipping known LLM-level fields.
// The result is sorted with preferred provider (matching llm.provider by api_type) first.
func discoverProviderNames(cfg interface {
	GetString(string) string
	Keys() []string
}) []string {
	knownFields := map[string]bool{
		"provider": true, "model": true, "temperature": true,
		"max_tokens": true, "max_retries": true, "timeout": true,
		"limit": true,
	}

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
		if knownFields[name] {
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

	// Sort with preferred provider first.
	// Matches llm.provider against: provider field > api_type > section name.
	if preferred := cfg.GetString("llm.provider"); preferred != "" {
		sort.SliceStable(providers, func(i, j int) bool {
			prefI := isPreferredProvider(cfg, providers[i], preferred)
			prefJ := isPreferredProvider(cfg, providers[j], preferred)
			if prefI && !prefJ {
				return true
			}
			if prefJ && !prefI {
				return false
			}
			return i < j
		})
	}

	return providers
}

// matchProvider checks if a provider section matches the preferred value.
// Checks: provider field > api_type > section name.
func isPreferredProvider(cfg interface{ GetString(string) string }, name, preferred string) bool {
	if cfg.GetString("llm."+name+".provider") == preferred {
		return true
	}
	if cfg.GetString("llm."+name+".api_type") == preferred {
		return true
	}
	return name == preferred
}

func parseProviderModels(cfg interface {
	GetString(string) string
	GetInt(string) int
	GetFloat(string) float64
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
		models = append(models, llm.ModelConfig{
			Name:        name,
			Provider:    provider,
			Vendor:      vendor,
			APIType:     apiType,
			Model:       name,
			MaxTokens:   cfg.GetInt(prefix + ".max_tokens"),
			Temperature: cfg.GetFloat(prefix + ".temperature"),
		})
	}
	return models
}

func (c *Context) createProvider(name string, models []llm.ModelConfig) llm.Provider {
	cfg := llm.Config{
		Provider:    name,
		Vendor:      c.Config.GetString("llm." + name + ".provider"),
		APIType:     c.Config.GetString("llm." + name + ".api_type"),
		Model:       c.Config.GetString("llm.model"),
		APIKey:      c.Config.GetString("llm." + name + ".api_key"),
		BaseURL:     c.Config.GetString("llm." + name + ".base_url"),
		Temperature: c.Config.GetFloat("llm.temperature"),
		MaxTokens:   c.Config.GetInt("llm.max_tokens"),
		MaxRetries:  c.Config.GetInt("llm.max_retries"),
		Timeout:     c.Config.GetDuration("llm.timeout"),
		Headers:     c.Config.GetStringMap("llm." + name + ".headers"),
	}
	if len(models) > 0 {
		cfg.Models = models
	}
	return llm.NewProvider(cfg, c.Logger)
}
