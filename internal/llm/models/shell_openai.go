// Package models registers one provider per (model, api_type) pair. Each model
// has its own file and its own registration; models with no special behavior
// delegate to the shells in this file, while models that differ override the
// relevant pieces in their own file. There is no automatic fallback: a model
// with no registered provider is a hard error at lookup time.
package models

import (
	"bytes"
	"context"
	"net/http"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	openaiproto "dolphin/internal/llm/proto/openai"
)

// findModelConfig returns the ModelConfig from cfg whose Name matches name.
// If absent (e.g. a section with no explicit model list), a minimal config is
// synthesized so the provider still reports a model identity.
func findModelConfig(cfg llm.Config, name string) llm.ModelConfig {
	for _, m := range cfg.Models {
		if m.Name == name {
			return m
		}
	}
	return llm.ModelConfig{Name: name, Model: name, Provider: cfg.Provider, APIType: cfg.APIType}
}

// mergedHeaders combines section-level headers (cfg.Headers) with model-level
// headers (mc.Headers). Model headers override same-named section headers;
// other section headers are preserved. Returns nil when neither is set so
// callers can skip the loop entirely.
func mergedHeaders(cfg llm.Config, mc llm.ModelConfig) map[string]string {
	if len(cfg.Headers) == 0 && len(mc.Headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg.Headers)+len(mc.Headers))
	for k, v := range cfg.Headers {
		out[k] = v
	}
	for k, v := range mc.Headers {
		out[k] = v // model overrides section
	}
	return out
}

// NewOpenAIProvider returns a factory for a model that speaks the OpenAI chat
// completions protocol with no model-specific quirks. The factory closes over
// the section config (base URL, auth, headers) supplied at boot.
func NewOpenAIProvider(modelName string) llm.ProviderFactory {
	return func(cfg llm.Config, log *zap.Logger) llm.Provider {
		mc := findModelConfig(cfg, modelName)
		return llm.ProviderFunc{
			Name_:  modelName,
			Model_: mc,
			Stream_: func(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				msgs := openaiproto.BuildMessages(req, log)
				body, err := openaiproto.BuildRequest(req.Model, msgs, cfg, req)
				if err != nil {
					return nil, err
				}
				httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
					openaiproto.ChatURL(cfg.BaseURL), bytes.NewReader(body))
				if err != nil {
					return nil, err
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
				for k, v := range mergedHeaders(cfg, mc) {
					httpReq.Header.Set(k, v)
				}
				if req.Stream {
					return proto.DoStream(ctx, httpReq, req.Timeout,
						openaiproto.NewChunkDecoder, openaiproto.DecodeError, log)
				}
				return proto.DoComplete(ctx, httpReq, req.Timeout,
					openaiproto.DecodeComplete, openaiproto.DecodeError)
			},
		}
	}
}
