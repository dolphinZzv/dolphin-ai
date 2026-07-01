package setup

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	llmmodels "dolphin/internal/llm/models" // register per-model providers + shell fallback
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
		c.Logger.Warn("no providers configured via llm.<name>.api_key")
	} else {
		for _, name := range providerNames {
			c.bootstrapSection(ctx, name, mgr)
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
	GetStringMap(string) map[string]string
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
			Headers:         cfg.GetStringMap(prefix + ".headers"),
		})
	}
	return models
}

// bootstrapSection wires every model declared under llm.<name> (plus any
// discovered models) into the manager. Each model resolves to an independent
// per-model provider via LookupModelProvider(model, api_type); models with no
// registered provider are skipped with a warning rather than silently falling
// back to a generic implementation.
func (c *Context) bootstrapSection(ctx context.Context, name string, mgr *llm.Manager) {
	models := parseProviderModels(c.Config, name)
	apiType := c.Config.GetString("llm." + name + ".api_type")
	if apiType == "" {
		apiType = c.Config.GetString("llm." + name + ".provider")
	}

	vendor := c.Config.GetString("llm." + name + ".provider")
	baseURL := c.Config.GetString("llm." + name + ".base_url")
	if baseURL == "" {
		baseURL = defaultBaseURL(vendor)
	}
	cfg := llm.Config{
		Provider:      name,
		Vendor:        vendor,
		APIType:       apiType,
		APIKey:        c.Config.GetString("llm." + name + ".api_key"),
		BaseURL:       baseURL,
		MaxTokens:     c.Config.GetInt("llm.max_tokens"),
		MaxRetries:    c.Config.GetInt("llm.max_retries"),
		Timeout:       c.Config.GetDuration("llm.timeout"),
		Headers:       c.Config.GetStringMap("llm." + name + ".headers"),
		ModelDiscover: c.Config.GetBool("llm." + name + ".model_discover"),
	}

	if len(models) == 0 && cfg.ModelDiscover {
		discovered, err := llm.DiscoverModels(ctx, cfg, c.Logger)
		if err != nil {
			c.Logger.Warn("model discovery failed",
				zap.String("provider", name), zap.Error(err))
		} else {
			c.Logger.Info("model discovery succeeded",
				zap.String("provider", name), zap.Int("count", len(discovered)))
			models = discovered
		}
	}
	cfg.Models = models

	for _, mc := range models {
		if mc.Disabled {
			continue
		}
		factory, err := llm.LookupModelProvider(mc.Name, apiType)
		if err != nil {
			// Fallback: use generic shell for dynamically discovered
			// models (e.g. from OpenRouter) with no dedicated per-model
			// provider file.
			switch strings.ToLower(apiType) {
			case "anthropic":
				factory = llmmodels.NewAnthropicProvider(mc.Name)
			case "openai-responses":
				factory = llmmodels.NewResponsesProvider(mc.Name)
			default:
				factory = llmmodels.NewOpenAIProvider(mc.Name)
			}
		}
		mgr.AddProvider(mc.Name, factory(cfg, c.Logger))
	}
}

// defaultBaseURL returns the built-in base URL for well-known providers when
// the user doesn't explicitly configure a base_url. Users can still override
// by setting llm.<name>.base_url in config.yaml.
func defaultBaseURL(vendor string) string {
	switch strings.ToLower(vendor) {
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "deepseek":
		return "https://api.deepseek.com"
	case "mimo":
		return "https://api.xiaomimimo.com"
	case "longcat":
		return "https://api.longcat.chat"
	case "glm":
		return "https://open.bigmodel.cn/api/paas/v4"
	case "minimax":
		return "https://api.minimax.chat/v1"
	case "moonshot":
		return "https://api.moonshot.cn/v1"
	}
	return ""
}
