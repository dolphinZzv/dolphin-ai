package models

import (
	"bytes"
	"context"
	"net/http"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	responsesproto "dolphin/internal/llm/proto/responses"
)

// NewResponsesProvider returns a factory for a model that speaks the OpenAI
// Responses API protocol (/v1/responses) with no model-specific quirks.
// The factory closes over the section config (base URL, auth, headers)
// supplied at boot.
func NewResponsesProvider(modelName string) llm.ProviderFactory {
	return func(cfg llm.Config, log *zap.Logger) llm.Provider {
		mc := findModelConfig(cfg, modelName)
		return llm.ProviderFunc{
			Name_:  modelName,
			Model_: mc,
			Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				input, instructions := responsesproto.BuildInput(req, log)
				body, err := responsesproto.BuildRequest(req.Model, input, instructions, cfg, req)
				if err != nil {
					return nil, err
				}
				httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
					responsesproto.ChatURL(cfg.BaseURL), bytes.NewReader(body))
				if err != nil {
					return nil, err
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
				for k, v := range providerHeaders(cfg) {
					httpReq.Header.Set(k, v)
				}
				for k, v := range mergedHeaders(cfg, mc) {
					httpReq.Header.Set(k, v)
				}
				if req.Stream {
					return proto.DoStream(ctx, httpReq, req.Timeout,
						responsesproto.NewChunkDecoder, responsesproto.DecodeError, log)
				}
				return proto.DoComplete(ctx, httpReq, req.Timeout,
					responsesproto.DecodeComplete, responsesproto.DecodeError)
			},
		}
	}
}
