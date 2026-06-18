package custom

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

func init() {
	llm.RegisterProvider("openai", func(cfg llm.Config, logger *zap.Logger) llm.Provider {
		return &openAIProvider{cfg: cfg, logger: logger}
	})
}

type openAIProvider struct {
	cfg    llm.Config
	logger *zap.Logger
}

func (p *openAIProvider) Name() string { return "openai" }

func (p *openAIProvider) Models(ctx context.Context) ([]llm.ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []llm.ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "openai",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

func (p *openAIProvider) chatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return baseURL + "/v1/chat/completions"
}

func (p *openAIProvider) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	messages := llm.BuildOpenAIMessages(req, p.logger)
	body, err := llm.BuildOpenAIRequest(req.Model, messages, p.cfg, req)
	if err != nil {
		return nil, err
	}
	url := p.chatURL(p.cfg.BaseURL)
	return llm.StreamOpenAI(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
}
