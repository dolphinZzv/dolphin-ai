package llm

import (
	"go.uber.org/zap"
)

func init() {
	RegisterProvider("deepseek/anthropic", func(cfg Config, logger *zap.Logger) Provider {
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.deepseek.com"
		}
		return &anthropicProvider{cfg: cfg, logger: logger}
	})
}
