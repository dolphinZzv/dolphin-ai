package observability

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"
)

func TestNewOTelHook(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	if h.Name() != "otel" {
		t.Fatalf("expected 'otel', got '%s'", h.Name())
	}
}

func TestOTelHook_TurnStartEnd(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()
	sessionID := "sess-1"

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnStart,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: sessionID,
		Payload:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_TurnStartError(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventTurnStart,
		SessionID: "sess-err",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnError,
		SessionID: "sess-err",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_TurnStartInterrupt(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	h.Handle(ctx, event.Event{Type: event.EventTurnStart, SessionID: "sess-int"})
	h.Handle(ctx, event.Event{Type: event.EventTurnInterrupt, SessionID: "sess-int"})
}

func TestOTelHook_LLMStartComplete(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMStart,
		SessionID: "sess-llm",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sess-llm",
		Payload: map[string]any{
			"tokens": 150,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_ToolStartComplete(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventToolStart,
		SessionID: "sess-tool",
		Payload:   map[string]any{"tool": "my_tool"},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventToolComplete,
		SessionID: "sess-tool",
		Payload: map[string]any{
			"is_error": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_ToolStartError(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	h.Handle(ctx, event.Event{
		Type:      event.EventToolStart,
		SessionID: "sess-tool-err",
		Payload:   map[string]any{"tool": "bad_tool"},
	})

	err := h.Handle(ctx, event.Event{
		Type:      event.EventToolError,
		SessionID: "sess-tool-err",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_PopNonExistentSpan(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)

	// These events try to pop a span that was never saved.
	for _, et := range []event.Type{
		event.EventLLMComplete,
		event.EventToolComplete,
		event.EventToolError,
		event.EventTurnComplete,
		event.EventTurnError,
		event.EventTurnInterrupt,
	} {
		err := h.Handle(context.Background(), event.Event{Type: et, SessionID: "no-span"})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", et, err)
		}
	}
}

func TestOTelHook_InnerBlocksViaDirectSave(t *testing.T) {
	// The normal event flow has mismatched event type between saveSpan (e.g. "llm.start")
	// and popSpan (e.g. "llm.complete"). To cover the inner blocks where span != nil,
	// we save spans manually with matching event types and then call Handle to pop them.
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	// --- LLMComplete inner block: span non-nil ---
	_, span := h.tracer.Start(ctx, "test-llm")
	h.saveSpan(event.Event{Type: event.EventLLMComplete, SessionID: "sess-llm-inner"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sess-llm-inner",
		Payload: map[string]any{
			"tokens": 42,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verify span was removed from the map.
	h.mu.Lock()
	_, exists := h.spans[spanKey(event.Event{Type: event.EventLLMComplete, SessionID: "sess-llm-inner"})]
	h.mu.Unlock()
	if exists {
		t.Fatal("span should have been popped and removed")
	}

	// --- ToolComplete inner block: with is_error=true ---
	_, span2 := h.tracer.Start(ctx, "test-tool")
	h.saveSpan(event.Event{Type: event.EventToolComplete, SessionID: "sess-tool-inner"}, span2)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventToolComplete,
		SessionID: "sess-tool-inner",
		Payload:   map[string]any{"is_error": true},
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- ToolComplete inner block: with is_error=false ---
	_, span3 := h.tracer.Start(ctx, "test-tool-ok")
	h.saveSpan(event.Event{Type: event.EventToolComplete, SessionID: "sess-tool-ok"}, span3)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventToolComplete,
		SessionID: "sess-tool-ok",
		Payload:   map[string]any{"is_error": false},
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- ToolError inner block ---
	_, span4 := h.tracer.Start(ctx, "test-tool-err")
	h.saveSpan(event.Event{Type: event.EventToolError, SessionID: "sess-tool-err-inner"}, span4)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventToolError,
		SessionID: "sess-tool-err-inner",
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- TurnComplete inner block ---
	_, span5 := h.tracer.Start(ctx, "test-turn")
	h.saveSpan(event.Event{Type: event.EventTurnComplete, SessionID: "sess-turn-inner"}, span5)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnComplete,
		SessionID: "sess-turn-inner",
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- TurnError inner block ---
	_, span6 := h.tracer.Start(ctx, "test-turn-err")
	h.saveSpan(event.Event{Type: event.EventTurnError, SessionID: "sess-turn-err-inner"}, span6)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnError,
		SessionID: "sess-turn-err-inner",
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- TurnInterrupt inner block ---
	_, span7 := h.tracer.Start(ctx, "test-turn-int")
	h.saveSpan(event.Event{Type: event.EventTurnInterrupt, SessionID: "sess-turn-int-inner"}, span7)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventTurnInterrupt,
		SessionID: "sess-turn-int-inner",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_LLMCompleteWithoutTokens(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	// LLMComplete with no "tokens" payload key should still end the span.
	_, span := h.tracer.Start(ctx, "test-llm-notok")
	h.saveSpan(event.Event{Type: event.EventLLMComplete, SessionID: "sess-llm-notok"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sess-llm-notok",
		Payload:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_LLMCompleteWithNonIntTokens(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	// Type assertion fails, span should still be ended.
	_, span := h.tracer.Start(ctx, "test-llm-float")
	h.saveSpan(event.Event{Type: event.EventLLMComplete, SessionID: "sess-llm-float"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sess-llm-float",
		Payload:   map[string]any{"tokens": "string_value"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_ToolCompleteWithoutIsError(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	// ToolComplete with no "is_error" payload key.
	_, span := h.tracer.Start(ctx, "test-tool-noerr")
	h.saveSpan(event.Event{Type: event.EventToolComplete, SessionID: "sess-tool-noerr"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventToolComplete,
		SessionID: "sess-tool-noerr",
		Payload:   map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_spanKey(t *testing.T) {
	id := spanKey(event.Event{Type: event.EventLLMStart, SessionID: "sess-1"})
	if id != "llm:sess-1" {
		t.Fatalf("expected 'llm:sess-1', got '%s'", id)
	}
}

func TestNewMetricsHook(t *testing.T) {
	mp := otel.GetMeterProvider()
	mh, err := NewMetricsHook(mp)
	if err != nil {
		t.Fatal(err)
	}
	if mh.Name() != "metrics" {
		t.Fatalf("expected 'metrics', got '%s'", mh.Name())
	}
}

func TestMetricsHook_TurnComplete(t *testing.T) {
	mh := newMetricsHookWithNoopMeter(t)
	ctx := context.Background()

	err := mh.Handle(ctx, event.Event{
		Type:    event.EventTurnComplete,
		Payload: map[string]any{"duration_ms": 123.4},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetricsHook_LLMComplete(t *testing.T) {
	mh := newMetricsHookWithNoopMeter(t)
	ctx := context.Background()

	err := mh.Handle(ctx, event.Event{
		Type:    event.EventLLMComplete,
		Payload: map[string]any{"tokens": 42},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetricsHook_ToolComplete(t *testing.T) {
	mh := newMetricsHookWithNoopMeter(t)
	ctx := context.Background()

	err := mh.Handle(ctx, event.Event{
		Type: event.EventToolComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetricsHook_IgnoredEventTypes(t *testing.T) {
	mh := newMetricsHookWithNoopMeter(t)
	ctx := context.Background()

	// Events that don't match any handler case should be silently ignored.
	for _, et := range []event.Type{
		event.EventTurnStart,
		event.EventTurnError,
		event.EventLLMStart,
		event.EventLLMError,
		event.EventToolStart,
		event.EventToolError,
	} {
		err := mh.Handle(ctx, event.Event{Type: et})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", et, err)
		}
	}
}

func newMetricsHookWithNoopMeter(t *testing.T) *MetricsHook {
	t.Helper()

	mp := otel.GetMeterProvider()
	mh, err := NewMetricsHook(mp)
	if err != nil {
		t.Fatal(err)
	}
	return mh
}

// Ensure the noop meter provider is available.
var _ metric.MeterProvider = otel.GetMeterProvider()

func TestBuildObservabilityDisabled(t *testing.T) {
	cfg := newTestConfig(t, "otel:\n  enabled: false\n")
	hr := hook.NewRegistry()
	BuildObservability(cfg, hr)
	// With otel.enabled=false, no hooks should be registered.
}

func TestBuildObservabilityEnabled(t *testing.T) {
	cfg := newTestConfig(t, "otel:\n  enabled: true\n")
	hr := hook.NewRegistry()
	BuildObservability(cfg, hr)
	// When enabled, hooks are registered (OTel SDK is noop by default).
}

// newTestConfig creates a temporary YAML file and loads the config.
func newTestConfig(t *testing.T, yamlContent string) *config.Config {
	t.Helper()

	f, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(baseConfigYAML + yamlContent); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.LoadConfig(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const baseConfigYAML = `
llm:
  provider: openai
  model: gpt-4
  openai:
    api_key: test-key
  timeout: 30s
tool:
  timeout: 30s
agent:
  max_rounds: 10
  buffer_size: 1024
memory:
  window: 10
`

// Unused import guard — these types are used within the test helpers.
var (
	_ = trace.Span(nil)
	_ = attribute.Bool("", false)
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello"},
		{"", 10, ""},
		{"abc", 0, ""},
		{"abcdef", 3, "abc"},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q; want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	tests := []struct {
		sid  string
		want string
	}{
		{"valid-session-1", "valid-session-1"},
		{"", ""},
		{string(make([]byte, 201)), ""},
		{"ascii-only", "ascii-only"},
		{"会话ID", ""},
	}
	for _, tc := range tests {
		got := validSessionID(tc.sid)
		if got != tc.want {
			t.Errorf("validSessionID(%q) = %q; want %q", tc.sid, got, tc.want)
		}
	}
}

func TestOTelHook_LLMStartWithPayload(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMStart,
		SessionID: "sess-llm-payload",
		Payload: map[string]any{
			"model": "gpt-4",
			"tools": []string{"tool1", "tool2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.Handle(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: "sess-llm-payload",
		Payload: map[string]any{
			"tokens":                      100,
			"input_tokens":                50,
			"output_tokens":               50,
			"total_tokens":                100,
			"cache_creation_input_tokens": 25,
			"cache_read_input_tokens":     25,
			"prompt_cached_tokens":        10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_LLMErrorWithMessage(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	_, span := h.tracer.Start(ctx, "test-llm-err")
	h.saveSpan(event.Event{Type: event.EventLLMError, SessionID: "sess-llm-err-msg"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMError,
		SessionID: "sess-llm-err-msg",
		Payload:   map[string]any{"error": "rate limit exceeded"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_LLMRetry(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	_, span := h.tracer.Start(ctx, "test-llm-retry")
	h.saveSpan(event.Event{Type: event.EventLLMRetry, SessionID: "sess-llm-retry"}, span)

	err := h.Handle(ctx, event.Event{
		Type:      event.EventLLMRetry,
		SessionID: "sess-llm-retry",
		Payload: map[string]any{
			"attempt": 2,
			"error":   "timeout",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOTelHook_ToolStartWithInput(t *testing.T) {
	tp := otel.GetTracerProvider()
	h := NewOTelHook(tp)
	ctx := context.Background()

	err := h.Handle(ctx, event.Event{
		Type:      event.EventToolStart,
		SessionID: "sess-tool-input",
		Payload: map[string]any{
			"tool":  "search",
			"input": "find something",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, span := h.tracer.Start(ctx, "test-tool-err-out")
	h.saveSpan(event.Event{Type: event.EventToolError, SessionID: "sess-tool-input"}, span)

	err = h.Handle(ctx, event.Event{
		Type:      event.EventToolError,
		SessionID: "sess-tool-input",
		Payload: map[string]any{
			"input": "find something",
			"error": "not found",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
