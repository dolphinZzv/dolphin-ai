package observability

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"dolphin/internal/event"
)

type MetricsHook struct {
	turnDuration    metric.Float64Histogram
	llmTokens       metric.Int64Counter
	toolCalls       metric.Int64Counter
	turnTotal       metric.Int64Counter
	cacheReadTokens metric.Int64Counter // cache read input tokens (prompt cached)
	cacheHitTokens  metric.Int64Counter // DeepSeek prompt_cache_hit_tokens
	cacheMissTokens metric.Int64Counter // DeepSeek prompt_cache_miss_tokens
}

func (h *MetricsHook) Name() string { return "metrics" }

func (h *MetricsHook) Handle(ctx context.Context, e event.Event) error {
	switch e.Type {
	case event.EventTurnComplete:
		if ms, ok := e.Payload["duration_ms"].(float64); ok {
			h.turnDuration.Record(ctx, ms)
		}
		h.turnTotal.Add(ctx, 1)

	case event.EventLLMComplete:
		if tokens, ok := e.Payload["tokens"].(int); ok {
			h.llmTokens.Add(ctx, int64(tokens))
		}
		if cacheRead, ok := e.Payload["cache_read_input_tokens"].(int); ok {
			h.cacheReadTokens.Add(ctx, int64(cacheRead))
		}
		if cachedTokens, ok := e.Payload["prompt_cached_tokens"].(int); ok {
			h.cacheReadTokens.Add(ctx, int64(cachedTokens))
		}

	case event.EventToolComplete:
		h.toolCalls.Add(ctx, 1)
	}
	return nil
}
