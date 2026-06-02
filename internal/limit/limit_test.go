package limit

import (
	"context"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"go.uber.org/zap"
)

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()

	// Get on empty key.
	v, err := s.Get("foo")
	if err != nil || v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}

	// Increment.
	v, err = s.Increment("foo", 1)
	if err != nil || v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
	v, _ = s.Increment("foo", 2)
	if v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}

	// Reset prefix.
	if err := s.Reset("foo"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Get("foo")
	if v != 0 {
		t.Fatalf("expected 0 after reset, got %d", v)
	}

	// GetAll.
	s.Increment("a.b", 10)
	s.Increment("a.c", 20)
	all, _ := s.GetAll()
	if all["a.b"] != 10 || all["a.c"] != 20 {
		t.Fatalf("unexpected GetAll result: %v", all)
	}

	// Increment with delta = 0.
	v, err = s.Increment("zero", 0)
	if err != nil || v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}

	// Reset empty prefix resets everything.
	s.Increment("x", 1)
	s.Increment("y", 2)
	if err := s.Reset(""); err != nil {
		t.Fatal(err)
	}
	all, _ = s.GetAll()
	if len(all) != 0 {
		t.Fatalf("expected empty store after reset all, got %v", all)
	}
}

func TestSoftDefault(t *testing.T) {
	if v := softDefault(100); v != 80 {
		t.Fatalf("expected 80, got %d", v)
	}
	if v := softDefault(0); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
	if v := softDefault(1); v != 0 {
		t.Fatalf("expected 0 (1*80/100=0), got %d", v)
	}
	if v := softDefault(10); v != 8 {
		t.Fatalf("expected 8, got %d", v)
	}
}

func cfgFromMap(m map[string]any) *config.Config {
	return config.LoadConfigFromMap(m)
}

func TestLimiterNoLimit(t *testing.T) {
	// No limits configured — everything should pass.
	cfg := cfgFromMap(nil)
	store := NewMemoryStore()
	bus := event.NewBus()
	logger, _ := zap.NewDevelopment()

	l := NewLimiter(store, cfg, bus, logger)
	err := l.Handle(context.Background(), event.Event{
		Type:      event.EventCheckLLM,
		SessionID: "test-session",
		Payload:   map[string]any{"model": "deepseek-v4-flash"},
	})
	if err != nil {
		t.Fatalf("expected nil error with no limits, got: %v", err)
	}
}

func TestLimiterWithHardLimitConfig(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests.hard": "2",
	})
	store := NewMemoryStore()
	bus := event.NewBus()
	logger, _ := zap.NewDevelopment()

	l := NewLimiter(store, cfg, bus, logger)

	ctx := context.Background()

	// First call should pass.
	if err := l.Handle(ctx, event.Event{
		Type:    event.EventCheckLLM,
		Payload: map[string]any{"model": "deepseek-v4-flash"},
	}); err != nil {
		t.Fatalf("first call should pass, got: %v", err)
	}
	// Set counter to 1 (just below limit).
	store.Increment("llm.requests", 1)

	// Second call at limit (current=1, hard=2, soft=1) should warn but pass.
	if err := l.Handle(ctx, event.Event{
		Type:    event.EventCheckLLM,
		Payload: map[string]any{"model": "deepseek-v4-flash"},
	}); err != nil {
		t.Fatalf("second call (at soft limit) should pass, got: %v", err)
	}

	// Set counter to 2 (at hard limit).
	store.Increment("llm.requests", 1)

	// Third call should be blocked.
	if err := l.Handle(ctx, event.Event{
		Type:    event.EventCheckLLM,
		Payload: map[string]any{"model": "deepseek-v4-flash"},
	}); err == nil {
		t.Fatal("third call should be blocked by hard limit")
	}
}

func TestRecordLLM(t *testing.T) {
	store := NewMemoryStore()
	bus := event.NewBus()
	logger, _ := zap.NewDevelopment()
	cfg := cfgFromMap(nil)

	l := NewLimiter(store, cfg, bus, logger)

	l.RecordLLM("deepseek-v4-flash", 100, 50)

	req, _ := store.Get("llm.requests")
	if req != 1 {
		t.Fatalf("expected 1 request, got %d", req)
	}
	tokens, _ := store.Get("llm.total_tokens")
	if tokens != 150 {
		t.Fatalf("expected 150 tokens, got %d", tokens)
	}
	inputTokens, _ := store.Get("llm.input_tokens")
	if inputTokens != 100 {
		t.Fatalf("expected 100 input tokens, got %d", inputTokens)
	}
	outputTokens, _ := store.Get("llm.output_tokens")
	if outputTokens != 50 {
		t.Fatalf("expected 50 output tokens, got %d", outputTokens)
	}

	// Per-model.
	modelReq, _ := store.Get("llm.model.deepseek-v4-flash.requests")
	if modelReq != 1 {
		t.Fatalf("expected 1 model request, got %d", modelReq)
	}
}

func TestReadHardLimit(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"test.scalar":   "100",
		"test.obj.hard": "200",
	})

	// Scalar format.
	if v := ReadHardLimit(cfg, "test.scalar"); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}

	// Object format.
	if v := ReadHardLimit(cfg, "test.obj"); v != 200 {
		t.Fatalf("expected 200, got %d", v)
	}

	// Not set.
	if v := ReadHardLimit(cfg, "test.nonexistent"); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}
}

func TestLimiterSkipWhenDisabled(t *testing.T) {
	cfg := cfgFromMap(nil)
	store := NewMemoryStore()
	bus := event.NewBus()
	logger, _ := zap.NewDevelopment()

	l := NewLimiter(store, cfg, bus, logger)
	err := l.Handle(context.Background(), event.Event{
		Type:    event.EventCheckLLM,
		Payload: map[string]any{"model": "test"},
	})
	if err != nil {
		t.Fatalf("should pass when no limits configured, got: %v", err)
	}
}

func TestHasPrefix(t *testing.T) {
	if !hasPrefix("llm.requests", "llm.") {
		t.Fatal("expected match")
	}
	if hasPrefix("llm.requests", "llmx") {
		t.Fatal("expected no match")
	}
	if !hasPrefix("anything", "") {
		t.Fatal("empty prefix should match everything")
	}
}
