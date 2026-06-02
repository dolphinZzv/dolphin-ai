package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/event"
	"dolphin/internal/permission"
	"dolphin/internal/types"
)

func TestRegisterPermissionTool(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	RegisterPermissionTool(r, ps, nil)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, d := range defs {
		if d.Name == "request_permission" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected request_permission in registered tools")
	}
}

func TestRegisterEmitEventTool(t *testing.T) {
	r := NewRegistry()
	bus := event.NewBus()
	RegisterEmitEventTool(r, bus)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, d := range defs {
		if d.Name == "emit_event" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected emit_event in registered tools")
	}
}

func TestEmitEvent(t *testing.T) {
	r := NewRegistry()
	bus := event.NewBus()
	RegisterEmitEventTool(r, bus)

	eventsReceived := 0
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		eventsReceived++
	})

	args, _ := json.Marshal(map[string]string{
		"name": "test-event",
		"desc": "test description",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-1", Name: "emit_event", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Event") {
		t.Errorf("expected 'Event' in response, got: %s", result.Content)
	}
	if eventsReceived != 1 {
		t.Errorf("expected 1 event, got %d", eventsReceived)
	}
}

func TestEmitEventInvalidArgs(t *testing.T) {
	r := NewRegistry()
	bus := event.NewBus()
	RegisterEmitEventTool(r, bus)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-2", Name: "emit_event", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestRequestPermissionNoTransport(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	RegisterPermissionTool(r, ps, nil)

	args, _ := json.Marshal(map[string]string{
		"tool_name": "some_tool",
		"reason":    "I need it",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError when getTransport is nil")
	}
	if !strings.Contains(result.Content, "not available") {
		t.Errorf("expected 'not available' error, got: %s", result.Content)
	}
}

func TestRequestPermissionInvalidArgs(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	RegisterPermissionTool(r, ps, nil)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "request_permission", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}
