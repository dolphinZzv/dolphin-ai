package brain

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dolphin/internal/event"

	"go.uber.org/zap"
)

func newTestBrain(t *testing.T) *Brain {
	t.Helper()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return b
}

func setupTestSubscription(t *testing.T, b *Brain, name, pattern, path, content string) {
	t.Helper()
	sub := Subscription{
		Name:         name,
		EventPattern: pattern,
		Enabled:      true,
		Content:      content,
	}
	if path != "" {
		sub.Filters = SubscriptionFilter{Path: path}
	}
	if err := WriteSubscription(context.Background(), b, sub); err != nil {
		t.Fatalf("WriteSubscription failed: %v", err)
	}
}

func newEngine(b *Brain, bus *event.Bus) *SubscriptionEngine {
	e := NewSubscriptionEngine(b, bus, zap.NewNop())
	e.cooldownPeriod = 0 // disabled by default for tests
	return e
}

func TestEngineMatchExact(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "test-sub", "file.update", "", "triggered")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{
		Type:      event.EventFileUpdate,
		Timestamp: time.Now(),
	})

	if triggered.Load() != 1 {
		t.Errorf("expected 1 trigger, got %d", triggered.Load())
	}
}

func TestEngineMatchGlob(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "glob-sub", "file.*", "", "triggered")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})
	bus.Publish(context.Background(), event.Event{Type: event.EventFileUpdate})
	bus.Publish(context.Background(), event.Event{Type: event.EventFileDelete})

	if triggered.Load() != 3 {
		t.Errorf("expected 3 triggers (all file.*), got %d", triggered.Load())
	}
}

func TestEngineMatchWildcard(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "catch-all", "*", "", "triggered")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventLLMStart})
	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})
	bus.Publish(context.Background(), event.Event{Type: event.EventPipelineStart})

	if triggered.Load() != 3 {
		t.Errorf("expected 3 triggers (wildcard), got %d", triggered.Load())
	}
}

func TestEngineNoMatchDifferentPattern(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "only-llm", "llm.*", "", "triggered")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})

	if triggered.Load() != 0 {
		t.Errorf("expected 0 triggers for non-matching event, got %d", triggered.Load())
	}
}

func TestEngineFilterPathMatch(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "path-sub", "file.*", "SOUL.md", "soul changed")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{
		Type: event.EventFileUpdate,
		Payload: map[string]any{
			"path": "SOUL.md",
		},
	})
	bus.Publish(context.Background(), event.Event{
		Type: event.EventFileUpdate,
		Payload: map[string]any{
			"path": "other.md",
		},
	})

	if triggered.Load() != 1 {
		t.Errorf("expected 1 trigger (only SOUL.md), got %d", triggered.Load())
	}
}

func TestEngineFilterPathGlob(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "glob-path", "file.*", "knowledge/*", "knowledge changed")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{
		Type:    event.EventFileCreate,
		Payload: map[string]any{"path": "knowledge/foo.md"},
	})
	bus.Publish(context.Background(), event.Event{
		Type:    event.EventFileCreate,
		Payload: map[string]any{"path": "rules/bar.md"},
	})

	if triggered.Load() != 1 {
		t.Errorf("expected 1 trigger (knowledge/* only), got %d", triggered.Load())
	}
}

func TestEngineFilterPathMissingPayload(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "filter-no-payload", "file.*", "*.md", "filtered")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	// Event without a "path" in payload — filter should not match
	bus.Publish(context.Background(), event.Event{
		Type:    event.EventFileCreate,
		Payload: map[string]any{"ext": ".md"},
	})

	if triggered.Load() != 0 {
		t.Errorf("expected 0 triggers (missing path in payload), got %d", triggered.Load())
	}
}

func TestEngineDisabledSubscription(t *testing.T) {
	b := newTestBrain(t)
	sub := Subscription{
		Name:         "disabled",
		EventPattern: "file.*",
		Enabled:      false,
		Content:      "should not trigger",
	}
	if err := WriteSubscription(context.Background(), b, sub); err != nil {
		t.Fatalf("WriteSubscription failed: %v", err)
	}

	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})

	if triggered.Load() != 0 {
		t.Errorf("expected 0 triggers for disabled subscription, got %d", triggered.Load())
	}
}

func TestEngineEmptyContentNoTrigger(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "no-content", "file.*", "", "")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	var triggered atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggered.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})

	if triggered.Load() != 0 {
		t.Errorf("expected 0 triggers for empty content, got %d", triggered.Load())
	}
}

func TestEngineNoSendTurnNoTrigger(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "no-callback", "file.*", "", "content")
	bus := event.NewBus()
	engine := newEngine(b, bus)
	engine.Start()
	defer engine.Stop()

	// SendTurn is nil — should not panic
	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})
}

func TestEngineCooldownSuppression(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "cooldown-test", "file.*", "", "triggered")
	bus := event.NewBus()
	engine := NewSubscriptionEngine(b, bus, zap.NewNop())
	engine.cooldownPeriod = 100 * time.Millisecond
	engine.Start()
	defer engine.Stop()

	var triggerCount atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggerCount.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})
	if triggerCount.Load() != 1 {
		t.Errorf("expected 1 trigger after first event, got %d", triggerCount.Load())
	}

	// Second event immediately — suppressed by cooldown
	bus.Publish(context.Background(), event.Event{Type: event.EventFileUpdate})
	if triggerCount.Load() != 1 {
		t.Errorf("expected 1 trigger (cooldown active), got %d", triggerCount.Load())
	}

	time.Sleep(150 * time.Millisecond)

	// After cooldown expires — should trigger again
	bus.Publish(context.Background(), event.Event{Type: event.EventFileDelete})
	if triggerCount.Load() != 2 {
		t.Errorf("expected 2 triggers after cooldown, got %d", triggerCount.Load())
	}
}

func TestEngineCooldownPerSubscription(t *testing.T) {
	b := newTestBrain(t)
	setupTestSubscription(t, b, "sub-a", "file.*", "", "a")
	setupTestSubscription(t, b, "sub-b", "file.*", "", "b")
	bus := event.NewBus()
	engine := NewSubscriptionEngine(b, bus, zap.NewNop())
	engine.cooldownPeriod = time.Hour // long cooldown
	engine.Start()
	defer engine.Stop()

	var triggerCount atomic.Int32
	engine.SendTurn = func(ctx context.Context, input string) {
		triggerCount.Add(1)
	}

	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})

	if n := triggerCount.Load(); n != 2 {
		t.Errorf("expected 2 triggers (both subs), got %d", n)
	}

	// Both should be suppressed on next event
	bus.Publish(context.Background(), event.Event{Type: event.EventFileUpdate})

	if n := triggerCount.Load(); n != 2 {
		t.Errorf("expected 2 triggers (both suppressed), got %d", n)
	}
}

func TestEngineBuildTriggerInput(t *testing.T) {
	b := newTestBrain(t)
	bus := event.NewBus()
	engine := NewSubscriptionEngine(b, bus, zap.NewNop())

	input := engine.buildTriggerInput(&Subscription{
		Name:    "test",
		Content: "Process this event.",
	}, event.Event{
		Type: event.EventLLMEmit,
		Payload: map[string]any{
			"name": "my-event",
			"desc": "Something happened",
		},
	})

	if !strings.Contains(input, "[Event: llm.emit]") {
		t.Errorf("expected event type in input, got: %s", input)
	}
	if !strings.Contains(input, "[name: my-event]") {
		t.Errorf("expected event name in input, got: %s", input)
	}
	if !strings.Contains(input, "[desc: Something happened]") {
		t.Errorf("expected event desc in input, got: %s", input)
	}
	if !strings.Contains(input, "Process this event.") {
		t.Errorf("expected subscription content in input, got: %s", input)
	}
}

func TestEngineBuildTriggerInputNoPayload(t *testing.T) {
	b := newTestBrain(t)
	bus := event.NewBus()
	engine := NewSubscriptionEngine(b, bus, zap.NewNop())

	input := engine.buildTriggerInput(&Subscription{
		Name:    "no-payload",
		Content: "Generic event.",
	}, event.Event{
		Type:    event.EventPipelineStart,
		Payload: nil,
	})

	if !strings.Contains(input, "[Event: pipeline.start]") {
		t.Errorf("expected event type, got: %s", input)
	}
	if !strings.Contains(input, "Generic event.") {
		t.Errorf("expected content, got: %s", input)
	}
}

func TestEngineStartStop(t *testing.T) {
	b := newTestBrain(t)
	bus := event.NewBus()
	engine := NewSubscriptionEngine(b, bus, zap.NewNop())

	// Should not panic before Start
	bus.Publish(context.Background(), event.Event{Type: event.EventFileCreate})

	engine.Start()
	if !engine.running.Load() {
		t.Error("expected engine running after Start")
	}

	engine.Start() // idempotent

	engine.Stop()
	if engine.running.Load() {
		t.Error("expected engine stopped after Stop")
	}

	engine.Stop() // idempotent
}
