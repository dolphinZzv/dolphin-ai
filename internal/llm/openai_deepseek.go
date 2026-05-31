package llm

import (
	"go.uber.org/zap"
)

func init() {
	RegisterProvider("deepseek/openai", func(cfg Config, logger *zap.Logger) Provider {
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.deepseek.com"
		}
		return &openAIProvider{cfg: cfg, logger: logger}
	})
}
