package models

import (
	"bytes"
	"context"
	"net/http"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	anthropicproto "dolphin/internal/llm/proto/anthropic"
)

// NewAnthropicProvider returns a factory for a model that speaks the Anthropic
// messages protocol with no model-specific quirks.
func NewAnthropicProvider(modelName string) llm.ProviderFactory {
	return func(cfg llm.Config, log *zap.Logger) llm.Provider {
		mc := findModelConfig(cfg, modelName)
		return llm.ProviderFunc{
			Name_:  modelName,
			Model_: mc,
			Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				msgs := anthropicproto.BuildMessages(req, log)
				body, err := anthropicproto.BuildRequest(req.Model, msgs, cfg, req)
				if err != nil {
					return nil, err
				}
				httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
					anthropicproto.ChatURL(cfg.BaseURL), bytes.NewReader(body))
				if err != nil {
					return nil, err
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("x-api-key", cfg.APIKey)
				httpReq.Header.Set("anthropic-version", "2023-06-01")
				for k, v := range cfg.Headers {
					httpReq.Header.Set(k, v)
				}
				if req.Stream {
					return proto.DoStream(ctx, httpReq, req.Timeout,
						anthropicproto.NewChunkDecoder, anthropicproto.DecodeError, log)
				}
				return proto.DoComplete(ctx, httpReq, req.Timeout,
					anthropicproto.DecodeComplete, anthropicproto.DecodeError)
			},
		}
	}
}
