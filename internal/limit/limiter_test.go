package limit

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"dolphin/internal/event"
)

func newTestLogger(t *testing.T) *zap.Logger {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	return logger
}

func checkEvent(model string) event.Event {
	return event.Event{
		Type:      event.EventCheckLLM,
		SessionID: "test-session",
		Payload:   map[string]any{"model": model},
	}
}

func TestLimiterName(t *testing.T) {
	l := NewLimiter(NewMemoryStore(), cfgFromMap(nil), event.NewBus(), newTestLogger(t))
	if got := l.Name(); got != "limit" {
		t.Fatalf("expected name 'limit', got %q", got)
	}
}

func TestHandleIgnoresNonCheckEvents(t *testing.T) {
	l := NewLimiter(NewMemoryStore(), cfgFromMap(nil), event.NewBus(), newTestLogger(t))
	types := []event.Type{
		event.EventLLMStart, event.EventLLMComplete, event.EventLLMError,
		event.EventToolStart, event.EventTurnStart, event.EventTurnComplete,
	}
	for _, typ := range types {
		if err := l.Handle(context.Background(), event.Event{Type: typ, Payload: map[string]any{"model": "x"}}); err != nil {
			t.Fatalf("non-check event %q should pass, got: %v", typ, err)
		}
	}
	fresh, _ := NewMemoryStore().GetAll()
	if len(fresh) != 0 {
		t.Fatal("store should be untouched for non-check events")
	}
}

func TestHandleWithoutModelPayload(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests.hard": "2"})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	if err := l.Handle(context.Background(), event.Event{Type: event.EventCheckLLM, Payload: map[string]any{}}); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestClearAlertedResetsSoftWarningState(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests.hard": "2"})
	store := NewMemoryStore()
	bus := event.NewBus()
	l := NewLimiter(store, cfg, bus, newTestLogger(t))
	store.Increment("llm.requests", 1)

	var softCount atomic.Int32
	bus.Subscribe(func(_ context.Context, e event.Event) {
		if e.Type == event.EventLimitSoftWarn {
			softCount.Add(1)
		}
	})

	_ = l.Handle(context.Background(), checkEvent("m1"))
	_ = l.Handle(context.Background(), checkEvent("m1"))
	if got := softCount.Load(); got != 1 {
		t.Fatalf("expected 1 soft warn before clear, got %d", got)
	}

	l.ClearAlerted()
	_ = l.Handle(context.Background(), checkEvent("m1"))
	if got := softCount.Load(); got != 2 {
		t.Fatalf("expected 2 soft warns after clear, got %d", got)
	}
}

func TestScanModelLimitsLoadsConfiguredEntries(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                          "k",
		"llm.openai.models.0.name":                    "gpt-4",
		"llm.openai.models.0.limit.max_requests":      "50",
		"llm.openai.models.0.limit.max_total_tokens":  "10000",
		"llm.openai.models.1.name":                    "gpt-3.5",
		"llm.openai.models.1.limit.max_requests.hard": "100",
		"llm.openai.models.1.limit.max_requests.soft": "80",
		"llm.openai.models.2.name":                    "gpt-4-mini",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	limits := l.ModelLimits()
	if _, ok := limits["openai/gpt-4"]; !ok {
		t.Fatalf("expected openai/gpt-4 in limits, got %v", limits)
	}
	if _, ok := limits["openai/gpt-3.5"]; !ok {
		t.Fatalf("expected openai/gpt-3.5 in limits, got %v", limits)
	}
	if _, ok := limits["openai/gpt-4-mini"]; ok {
		t.Fatalf("openai/gpt-4-mini has no limits, should not be loaded")
	}
}

func TestDiscoverProviderSections(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":    "k",
		"llm.anthropic.api_key": "k2",
		"llm.deepseek.api_key":  "k3",
		"llm.openai.provider":   "openai",
		"llm.openai.model":      "x",
		"llm.temperature":       "0.5",
		"foo.bar.api_key":       "k4",
		"llm..api_key":          "k5",
		"llm.a.b.api_key":       "k6",
	})
	got := discoverProviderSections(cfg)
	want := map[string]bool{"openai": true, "anthropic": true, "deepseek": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d sections, got %d: %v", len(want), len(got), got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected section %q", s)
		}
	}
}

func TestDiscoverProviderSectionsNoApiKey(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.foo.bar": "baz"})
	if got := discoverProviderSections(cfg); len(got) != 0 {
		t.Fatalf("expected no sections, got %v", got)
	}
}

func TestGatherLimitsGlobal(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests":      "10",
		"llm.limit.max_total_tokens":  "1000",
		"llm.limit.max_input_tokens":  "500",
		"llm.limit.max_output_tokens": "500",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	defs := l.gatherLimits("any-model")
	keys := map[string]bool{}
	for _, d := range defs {
		keys[d.key] = true
	}
	for _, k := range []string{"llm.requests", "llm.total_tokens", "llm.input_tokens", "llm.output_tokens"} {
		if !keys[k] {
			t.Fatalf("expected limit key %q in %v", k, keys)
		}
	}
}

func TestGatherLimitsQualifiedExactMatch(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "5",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	defs := l.gatherLimits("openai/gpt-4")
	if len(defs) != 1 || defs[0].key != "llm.model.openai/gpt-4.requests" {
		t.Fatalf("expected 1 limit for openai/gpt-4, got %+v", defs)
	}
	defs = l.gatherLimits("anthropic/gpt-4")
	if len(defs) != 0 {
		t.Fatalf("expected no limits for anthropic/gpt-4, got %+v", defs)
	}
}

func TestGatherLimitsShortNameSuffixMatch(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                         "k",
		"llm.openai.models.0.name":                   "gpt-4",
		"llm.openai.models.0.limit.max_requests":     "5",
		"llm.openai.models.0.limit.max_total_tokens": "1000",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	defs := l.gatherLimits("gpt-4")
	if len(defs) != 2 {
		t.Fatalf("expected 2 limits (req+tokens) for gpt-4, got %+v", defs)
	}
}

func TestGatherLimitsPerModelSoftDefault(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                         "k",
		"llm.openai.models.0.name":                   "gpt-4",
		"llm.openai.models.0.limit.max_requests":     "100",
		"llm.openai.models.0.limit.max_total_tokens": "1000",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	for _, d := range l.gatherLimits("gpt-4") {
		if d.soft == 0 {
			t.Fatalf("expected non-zero soft default for %s, got %+v", d.key, d)
		}
	}
}

func TestGlobalLimitNilWithoutHard(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests.soft": "5"})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	if got := l.globalLimit("max_requests", "llm.requests", "requests"); got != nil {
		t.Fatalf("expected nil when no hard limit, got %+v", got)
	}
}

func TestGlobalLimitSoftFromObject(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests.hard": "100",
		"llm.limit.max_requests.soft": "60",
	})
	l := NewLimiter(NewMemoryStore(), cfg, event.NewBus(), newTestLogger(t))
	got := l.globalLimit("max_requests", "llm.requests", "requests")
	if len(got) != 1 || got[0].hard != 100 || got[0].soft != 60 {
		t.Fatalf("unexpected globalLimit result: %+v", got)
	}
}

func TestCheckLLMSoftObjectFormat(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests.hard": "10",
		"llm.limit.max_requests.soft": "5",
	})
	store := NewMemoryStore()
	bus := event.NewBus()
	l := NewLimiter(store, cfg, bus, newTestLogger(t))

	var softCount, hardCount int32
	bus.Subscribe(func(_ context.Context, e event.Event) {
		switch e.Type { //nolint:exhaustive // test counts only limit events
		case event.EventLimitSoftWarn:
			atomic.AddInt32(&softCount, 1)
		case event.EventLimitHardBlock:
			atomic.AddInt32(&hardCount, 1)
		}
	})
	store.Increment("llm.requests", 5)
	if err := l.Handle(context.Background(), checkEvent("m")); err != nil {
		t.Fatalf("at soft: expected pass, got: %v", err)
	}
	if atomic.LoadInt32(&softCount) != 1 {
		t.Fatalf("expected 1 soft warn, got %d", softCount)
	}
	store.Increment("llm.requests", 5)
	if err := l.Handle(context.Background(), checkEvent("m")); err == nil {
		t.Fatal("at hard: expected block, got nil")
	}
	if atomic.LoadInt32(&hardCount) != 1 {
		t.Fatalf("expected 1 hard block event, got %d", hardCount)
	}
}

func TestCheckLLMSoftDefaultFromScalar(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests": "10"})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	store.Increment("llm.requests", 8)
	if err := l.Handle(context.Background(), checkEvent("m")); err != nil {
		t.Fatalf("at default soft=8: expected pass, got: %v", err)
	}
	store.Increment("llm.requests", 2)
	if err := l.Handle(context.Background(), checkEvent("m")); err == nil {
		t.Fatal("at hard: expected block, got nil")
	}
}

func TestCheckLLMHardLimitErrorMessage(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests": "1"})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	store.Increment("llm.requests", 1)
	err := l.Handle(context.Background(), checkEvent("m"))
	if err == nil {
		t.Fatal("expected block")
	}
	if !strings.Contains(err.Error(), "requests") {
		t.Fatalf("error should mention metric name, got: %v", err)
	}
}

func TestCheckLLMFailsClosedOnStoreError(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"llm.limit.max_requests": "1"})
	store := &errStore{err: errBoom}
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	if err := l.Handle(context.Background(), checkEvent("m")); err == nil {
		t.Fatal("expected block when store errors and hard limit configured")
	}
}

func TestCheckLLMEventPayload(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests.hard": "1",
		"llm.limit.max_requests.soft": "1",
	})
	store := NewMemoryStore()
	bus := event.NewBus()
	l := NewLimiter(store, cfg, bus, newTestLogger(t))
	store.Increment("llm.requests", 1)
	var got event.Event
	bus.Subscribe(func(_ context.Context, e event.Event) {
		if e.Type == event.EventLimitHardBlock {
			got = e
		}
	})
	_ = l.Handle(context.Background(), checkEvent("m1"))
	if got.Type != event.EventLimitHardBlock {
		t.Fatalf("expected hard block event, got %v", got)
	}
	if got.SessionID != "test-session" {
		t.Fatalf("session id not propagated: %q", got.SessionID)
	}
	if got.Payload["model"] != "m1" {
		t.Fatalf("model not in payload: %v", got.Payload)
	}
	if got.Payload["hard"].(int64) != 1 {
		t.Fatalf("hard value wrong: %v", got.Payload["hard"])
	}
}

func TestCheckLLMReportsAllHardBreaches(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.limit.max_requests":     "1",
		"llm.limit.max_total_tokens": "1",
	})
	store := NewMemoryStore()
	bus := event.NewBus()
	l := NewLimiter(store, cfg, bus, newTestLogger(t))
	store.Increment("llm.requests", 1)
	store.Increment("llm.total_tokens", 1)
	var blocks int32
	bus.Subscribe(func(_ context.Context, e event.Event) {
		if e.Type == event.EventLimitHardBlock {
			atomic.AddInt32(&blocks, 1)
		}
	})
	err := l.Handle(context.Background(), checkEvent("m"))
	if err == nil {
		t.Fatal("expected block")
	}
	if atomic.LoadInt32(&blocks) != 2 {
		t.Fatalf("expected 2 hard block events (one per metric), got %d", blocks)
	}
	if !strings.Contains(err.Error(), "requests") || !strings.Contains(err.Error(), "total tokens") {
		t.Fatalf("error should mention all breached metrics, got: %v", err)
	}
}

func TestRecordLLMEmptyModel(t *testing.T) {
	store := NewMemoryStore()
	l := NewLimiter(store, cfgFromMap(nil), event.NewBus(), newTestLogger(t))
	l.RecordLLM("", 0, 0)
	if v, _ := store.Get("llm.requests"); v != 1 {
		t.Fatalf("expected 1 request, got %d", v)
	}
	all, _ := store.GetAll()
	for k := range all {
		if strings.HasPrefix(k, "llm.model.") {
			t.Fatalf("expected no per-model keys for empty model, got %q", k)
		}
	}
}

func TestRecordLLMZeroTokens(t *testing.T) {
	store := NewMemoryStore()
	l := NewLimiter(store, cfgFromMap(nil), event.NewBus(), newTestLogger(t))
	l.RecordLLM("m", 0, 0)
	if v, _ := store.Get("llm.total_tokens"); v != 0 {
		t.Fatalf("total tokens should stay 0, got %d", v)
	}
	if v, _ := store.Get("llm.input_tokens"); v != 0 {
		t.Fatalf("input tokens should stay 0, got %d", v)
	}
}

func TestRecordLLMQualifiedModelName(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "5",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	l.RecordLLM("openai/gpt-4", 10, 20)
	if v, _ := store.Get("llm.model.openai/gpt-4.requests"); v != 1 {
		t.Fatalf("qualified key missing, got %d", v)
	}
}

func TestRecordLLMUnqualifiedMatchesProvider(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "5",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	l.RecordLLM("gpt-4", 10, 20)
	if v, _ := store.Get("llm.model.gpt-4.requests"); v != 1 {
		t.Fatalf("unqualified key missing, got %d", v)
	}
	if v, _ := store.Get("llm.model.openai/gpt-4.requests"); v != 1 {
		t.Fatalf("qualified key missing, got %d", v)
	}
	if v, _ := store.Get("llm.model.openai/gpt-4.tokens"); v != 30 {
		t.Fatalf("qualified token key wrong, got %d", v)
	}
}

func TestRecordLLMUnqualifiedNoMatch(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "5",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	l.RecordLLM("other-model", 5, 5)
	if v, _ := store.Get("llm.model.other-model.requests"); v != 1 {
		t.Fatalf("expected 1 unqualified request, got %d", v)
	}
	all, _ := store.GetAll()
	for k := range all {
		if strings.Contains(k, "openai") {
			t.Fatalf("did not expect openai key for unrelated model, got %q", k)
		}
	}
}

func TestLimiterGetters(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"llm.openai.api_key":                     "k",
		"llm.openai.models.0.name":               "gpt-4",
		"llm.openai.models.0.limit.max_requests": "5",
	})
	store := NewMemoryStore()
	l := NewLimiter(store, cfg, event.NewBus(), newTestLogger(t))
	if l.Store() != store {
		t.Fatal("Store() should return same instance")
	}
	if l.Config() != cfg {
		t.Fatal("Config() should return same instance")
	}
	ml := l.ModelLimits()
	ml["injected"] = PerModelLimit{}
	if _, ok := l.ModelLimits()["injected"]; ok {
		t.Fatal("ModelLimits() should return a defensive copy")
	}
}

func TestReadSoftLimit(t *testing.T) {
	cfg := cfgFromMap(map[string]any{
		"a.soft":   "30",
		"b.hard":   "100",
		"b.soft":   "70",
		"c.scalar": "50",
	})
	if v := ReadSoftLimit(cfg, "a"); v != 30 {
		t.Fatalf("a.soft=30 expected, got %d", v)
	}
	if v := ReadSoftLimit(cfg, "b"); v != 70 {
		t.Fatalf("b.soft=70 expected, got %d", v)
	}
	if v := ReadSoftLimit(cfg, "c"); v != 0 {
		t.Fatalf("scalar should return 0 for soft, got %d", v)
	}
	if v := ReadSoftLimit(cfg, "missing"); v != 0 {
		t.Fatalf("missing key should return 0, got %d", v)
	}
}

func TestReadHardLimitZero(t *testing.T) {
	cfg := cfgFromMap(map[string]any{"x.hard": "0"})
	if v := ReadHardLimit(cfg, "x"); v != 0 {
		t.Fatalf("zero value should return 0, got %d", v)
	}
}

func TestSoftDefaultEdges(t *testing.T) {
	if v := softDefault(1_000_000); v != 800_000 {
		t.Fatalf("expected 800000, got %d", v)
	}
	if v := softDefault(5); v != 4 {
		t.Fatalf("5*80/100=4 expected, got %d", v)
	}
	if v := softDefault(-1); v != 0 {
		t.Fatalf("negative hard should return 0, got %d", v)
	}
}

type errStore struct{ err error }

func (s *errStore) Get(string) (int64, error)              { return 0, s.err }
func (s *errStore) Increment(string, int64) (int64, error) { return 0, s.err }
func (s *errStore) Reset(string) error                     { return s.err }
func (s *errStore) GetAll() (map[string]int64, error)      { return nil, s.err }

var errBoom = &boomErr{}

type boomErr struct{}

func (e *boomErr) Error() string { return "boom" }
