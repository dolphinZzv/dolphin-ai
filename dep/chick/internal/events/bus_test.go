package events

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscribeAndPublish(t *testing.T) {
	bus := NewBus()

	var called int32
	bus.Subscribe(EventIssueCreated, func(e Event) {
		atomic.AddInt32(&called, 1)
	})

	bus.Publish(Event{Type: EventIssueCreated, Payload: nil})
	bus.Publish(Event{Type: EventIssueCreated, Payload: nil})
	bus.Publish(Event{Type: EventIssueCreated, Payload: nil})

	time.Sleep(50 * time.Millisecond)

	if n := atomic.LoadInt32(&called); n != 3 {
		t.Errorf("expected 3 calls, got %d", n)
	}
}

func TestPublishSync(t *testing.T) {
	bus := NewBus()

	var called int32
	bus.Subscribe(EventIssueCreated, func(e Event) {
		atomic.AddInt32(&called, 1)
	})

	bus.PublishSync(Event{Type: EventIssueCreated})
	if n := atomic.LoadInt32(&called); n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestFilteredEvents(t *testing.T) {
	bus := NewBus()

	var issueCreatedCount, stateChangedCount int32
	bus.Subscribe(EventIssueCreated, func(e Event) {
		atomic.AddInt32(&issueCreatedCount, 1)
	})
	bus.Subscribe(EventIssueStateChanged, func(e Event) {
		atomic.AddInt32(&stateChangedCount, 1)
	})

	bus.PublishSync(Event{Type: EventIssueCreated})
	bus.PublishSync(Event{Type: EventIssueStateChanged})

	if n := atomic.LoadInt32(&issueCreatedCount); n != 1 {
		t.Errorf("expected 1 issue created, got %d", n)
	}
	if n := atomic.LoadInt32(&stateChangedCount); n != 1 {
		t.Errorf("expected 1 state changed, got %d", n)
	}
}

func TestEventPayload(t *testing.T) {
	bus := NewBus()

	var received Event
	bus.Subscribe(EventIssueCreated, func(e Event) {
		received = e
	})

	payload := map[string]interface{}{"key": "value", "num": 42}
	bus.PublishSync(Event{Type: EventIssueCreated, Payload: payload})

	if received.Type != EventIssueCreated {
		t.Errorf("expected IssueCreated, got %s", received.Type)
	}
	p, ok := received.Payload.(map[string]interface{})
	if !ok {
		t.Fatal("payload is not map")
	}
	if p["key"] != "value" {
		t.Errorf("expected 'value', got %v", p["key"])
	}
	if p["num"] != 42 {
		t.Errorf("expected 42, got %v", p["num"])
	}
}
