package limit

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"dolphin/internal/event"
)

func TestResetUsage_AllClearsGlobalAndPerModel(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                         "k",
		"llm.openai.models.0.name":                   "gpt-4",
		"llm.openai.models.0.limit.max_requests":     "50",
		"llm.openai.models.0.limit.max_total_tokens": "10000",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))

	// Record some usage.
	store.Increment("llm.requests", 5)
	store.Increment("llm.total_tokens", 100)
	store.Increment("llm.model.openai/gpt-4.requests", 3)
	store.Increment("llm.model.openai/gpt-4.tokens", 90)

	if n, err := l.ResetUsage(""); err != nil {
		t.Fatalf("ResetUsage(\"\") error: %v", err)
	} else if n != 0 {
		t.Errorf("ResetUsage(\"\") should report 0 (all), got %d", n)
	}

	all, _ := store.GetAll()
	for k, v := range all {
		if v != 0 {
			t.Errorf("after full reset, key %s = %d, want 0", k, v)
		}
	}
}

func TestResetUsage_QualifiedModelOnly(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "50",
		"llm.openai.models.1.name":               "gpt-3.5",
		"llm.openai.models.1.limit.max_requests": "30",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))

	store.Increment("llm.model.openai/gpt-4.requests", 3)
	store.Increment("llm.model.openai/gpt-3.5.requests", 7)
	store.Increment("llm.requests", 10)

	n, err := l.ResetUsage("openai/gpt-4")
	if err != nil {
		t.Fatalf("ResetUsage error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 key reset, got %d", n)
	}

	all, _ := store.GetAll()
	if all["llm.model.openai/gpt-4.requests"] != 0 {
		t.Errorf("gpt-4 requests should be reset, got %d", all["llm.model.openai/gpt-4.requests"])
	}
	if all["llm.model.openai/gpt-3.5.requests"] != 7 {
		t.Errorf("gpt-3.5 requests should be untouched, got %d", all["llm.model.openai/gpt-3.5.requests"])
	}
	if all["llm.requests"] != 10 {
		t.Errorf("global requests should be untouched, got %d", all["llm.requests"])
	}
}

func TestResetUsage_ShortNameMatchesAcrossProviders(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "50",
		"llm.ark.api_key":                        "k",
		"llm.ark.models.0.name":                  "gpt-4",
		"llm.ark.models.0.limit.max_requests":    "40",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))

	store.Increment("llm.model.openai/gpt-4.requests", 3)
	store.Increment("llm.model.ark/gpt-4.requests", 5)

	n, err := l.ResetUsage("gpt-4")
	if err != nil {
		t.Fatalf("ResetUsage error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 keys reset (both providers), got %d", n)
	}

	all, _ := store.GetAll()
	if all["llm.model.openai/gpt-4.requests"] != 0 || all["llm.model.ark/gpt-4.requests"] != 0 {
		t.Errorf("both gpt-4 entries should be reset: %+v", all)
	}
}

func TestResetUsage_VendorPrefixFallback(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "50",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))

	store.Increment("llm.model.openai/gpt-4.requests", 3)

	// "openai" is not a model name but matches the qualified-name prefix.
	n, err := l.ResetUsage("openai")
	if err != nil {
		t.Fatalf("ResetUsage error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 key reset via vendor prefix, got %d", n)
	}

	all, _ := store.GetAll()
	if all["llm.model.openai/gpt-4.requests"] != 0 {
		t.Errorf("openai/gpt-4 should be reset, got %d", all["llm.model.openai/gpt-4.requests"])
	}
}

func TestResetUsage_ClearsAlertedEntriesForResetKeys(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "2",
	})
	store := NewMemoryStore()
	bus := event.NewBus()
	l := NewLimiter(store, cfg, bus, zap.NewNop())

	// Trigger a hard-limit alert by exceeding the limit (max_requests=2).
	store.Increment("llm.model.openai/gpt-4.requests", 3)
	if err := l.Handle(context.Background(), checkEvent("gpt-4")); err == nil {
		t.Fatal("expected hard-limit error on first check")
	}
	// alerted map should now contain the hard alert key.
	alertKey := "llm.model.openai/gpt-4.requests/hard"
	if !l.alerted[alertKey] {
		t.Fatal("expected alerted entry for gpt-4 hard limit")
	}

	// Reset usage for gpt-4 — the alert entry should be cleared.
	if _, err := l.ResetUsage("openai/gpt-4"); err != nil {
		t.Fatalf("ResetUsage error: %v", err)
	}
	if l.alerted[alertKey] {
		t.Error("alerted entry should be cleared after reset")
	}
}
