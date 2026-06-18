package observability

import (
	"context"
	"strings"
	"sync"
	"unicode"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"dolphin/internal/event"
)

type OTelHook struct {
	tracer trace.Tracer
	spans  map[string]trace.Span
	mu     sync.Mutex
}

func NewOTelHook(tp trace.TracerProvider) *OTelHook {
	return &OTelHook{
		tracer: tp.Tracer("dolphin"),
		spans:  make(map[string]trace.Span),
	}
}

func (h *OTelHook) Name() string { return "otel" }

func (h *OTelHook) Handle(ctx context.Context, e event.Event) error {
	sid := validSessionID(e.SessionID)

	switch e.Type { //nolint:exhaustive // traces only LLM/tool spans
	case event.EventLLMStart:
		_, span := h.tracer.Start(ctx, "llm.complete")
		if sid != "" {
			span.SetAttributes(attribute.String("sessionid", sid))
		}
		if model, ok := e.Payload["model"].(string); ok {
			span.SetAttributes(attribute.String("model", model))
		}
		if tools, ok := e.Payload["tools"].([]string); ok {
			span.SetAttributes(attribute.StringSlice("tools", tools))
		}
		h.saveSpan(e, span)

	case event.EventLLMComplete:
		if span := h.popSpan(e); span != nil {
			if tokens, ok := e.Payload["tokens"].(int); ok {
				span.SetAttributes(attribute.Int("tokens", tokens))
			}
			if in, ok := e.Payload["input_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("input_tokens", in))
			}
			if out, ok := e.Payload["output_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("output_tokens", out))
			}
			if total, ok := e.Payload["total_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("total_tokens", total))
			}
			if cacheCreate, ok := e.Payload["cache_creation_input_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("cache_creation_input_tokens", cacheCreate))
			}
			if cacheRead, ok := e.Payload["cache_read_input_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("cache_read_input_tokens", cacheRead))
			}
			if cachedTokens, ok := e.Payload["prompt_cached_tokens"].(int); ok {
				span.SetAttributes(attribute.Int("prompt_cached_tokens", cachedTokens))
			}
			if sid != "" {
				span.SetAttributes(attribute.String("sessionid", sid))
			}
			span.End()
		}

	case event.EventLLMError:
		if span := h.popSpan(e); span != nil {
			span.SetAttributes(attribute.Bool("error", true))
			if errMsg, ok := e.Payload["error"].(string); ok {
				span.SetStatus(codes.Error, errMsg)
				span.SetAttributes(attribute.String("error.message", errMsg))
			}
			if sid != "" {
				span.SetAttributes(attribute.String("sessionid", sid))
			}
			span.End()
		}

	case event.EventLLMRetry:
		if span := h.getSpan(e); span != nil {
			if attempt, ok := e.Payload["attempt"].(int); ok {
				span.SetAttributes(attribute.Int("retry_attempt", attempt))
			}
			if errMsg, ok := e.Payload["error"].(string); ok {
				span.SetAttributes(attribute.String("retry_error", errMsg))
			}
		}

	case event.EventToolStart:
		name, _ := e.Payload["tool"].(string)
		_, span := h.tracer.Start(ctx, "tool."+name)
		if sid != "" {
			span.SetAttributes(attribute.String("sessionid", sid))
		}
		if input, ok := e.Payload["input"].(string); ok {
			span.SetAttributes(attribute.String("input", truncate(input, 4096)))
		}
		h.saveSpan(e, span)

	case event.EventToolComplete:
		if span := h.popSpan(e); span != nil {
			if isErr, ok := e.Payload["is_error"].(bool); ok {
				span.SetAttributes(attribute.Bool("error", isErr))
			}
			if output, ok := e.Payload["output"].(string); ok {
				span.SetAttributes(attribute.String("output", truncate(output, 4096)))
			}
			if sid != "" {
				span.SetAttributes(attribute.String("sessionid", sid))
			}
			span.End()
		}

	case event.EventToolError:
		if span := h.popSpan(e); span != nil {
			span.SetAttributes(attribute.Bool("error", true))
			if input, ok := e.Payload["input"].(string); ok {
				span.SetAttributes(attribute.String("input", truncate(input, 4096)))
			}
			if errMsg, ok := e.Payload["error"].(string); ok {
				span.SetStatus(codes.Error, errMsg)
				span.SetAttributes(attribute.String("error.message", errMsg))
			}
			if sid != "" {
				span.SetAttributes(attribute.String("sessionid", sid))
			}
			span.End()
		}
	}
	return nil
}

func (h *OTelHook) saveSpan(e event.Event, span trace.Span) {
	h.mu.Lock()
	h.spans[spanKey(e)] = span
	h.mu.Unlock()
}

func (h *OTelHook) popSpan(e event.Event) trace.Span {
	h.mu.Lock()
	span := h.spans[spanKey(e)]
	delete(h.spans, spanKey(e))
	h.mu.Unlock()
	return span
}

func (h *OTelHook) getSpan(e event.Event) trace.Span {
	h.mu.Lock()
	span := h.spans[spanKey(e)]
	h.mu.Unlock()
	return span
}

func spanKey(e event.Event) string {
	category, _, _ := strings.Cut(string(e.Type), ".")
	return category + ":" + e.SessionID
}

// validSessionID validates and returns a session ID that is:
// - US-ASCII only (printable or whitespace)
// - max 200 characters
// Returns empty string if the session ID is invalid.
func validSessionID(sid string) string {
	if len(sid) > 200 {
		return ""
	}
	for _, r := range sid {
		if r > unicode.MaxASCII {
			return ""
		}
	}
	return sid
}

// truncate returns s truncated to at most max bytes.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
