package deepseek

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func init() {
	llm.RegisterProvider("deepseek/anthropic", func(cfg llm.Config, logger *zap.Logger) llm.Provider {
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.deepseek.com"
		}
		return &anthropicProvider{cfg: cfg, logger: logger}
	})
}

type anthropicProvider struct {
	cfg    llm.Config
	logger *zap.Logger
}

func (p *anthropicProvider) Name() string { return "deepseek" }

func (p *anthropicProvider) Models(ctx context.Context) ([]llm.ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []llm.ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "deepseek",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

func (p *anthropicProvider) chatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	return baseURL + "/v1/messages"
}

func (p *anthropicProvider) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	messages := llm.BuildAnthropicMessages(req, p.logger)
	body, err := llm.BuildAnthropicRequest(req.Model, messages, p.cfg, req)
	if err != nil {
		return nil, err
	}
	url := p.chatURL(p.cfg.BaseURL)
	return llm.StreamAnthropic(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
}
