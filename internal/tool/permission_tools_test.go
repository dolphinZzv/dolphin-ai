package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/common"
	"dolphin/internal/event"
	"dolphin/internal/permission"
	"dolphin/internal/session"
	"dolphin/internal/transport"
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

type mockPermTransport struct {
	id               string
	permissionResult transport.PermissionResult
}

func (m *mockPermTransport) ID() string                      { return m.id }
func (m *mockPermTransport) Start(ctx context.Context) error { return nil }
func (m *mockPermTransport) Read(ctx context.Context) (transport.Input, error) {
	return transport.Input{}, nil
}
func (m *mockPermTransport) Write(ctx context.Context, text string) error    { return nil }
func (m *mockPermTransport) Flush() error                                    { return nil }
func (m *mockPermTransport) Close() error                                    { return nil }
func (m *mockPermTransport) Context() string                                 { return "" }
func (m *mockPermTransport) Tools() []common.ToolDesc                        { return nil }
func (m *mockPermTransport) Capability() transport.Capability                { return transport.Capability{} }
func (m *mockPermTransport) NewSession(ctx context.Context) *session.Session { return nil }
func (m *mockPermTransport) Session() *session.Session                       { return nil }
func (m *mockPermTransport) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return m.permissionResult, nil
}

func (m *mockPermTransport) Confirm(_ context.Context, _ string) (bool, error) {
	return m.permissionResult != transport.PermissionDenied, nil
}
func (m *mockPermTransport) WriteThinking(_ context.Context, _ string) error             { return nil }
func (m *mockPermTransport) WriteToolCall(_ context.Context, _ types.ToolCall) error     { return nil }
func (m *mockPermTransport) WriteToolResult(_ context.Context, _ types.ToolResult) error { return nil }

func TestRequestPermissionDenied(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	getTransport := func(id string) transport.IO {
		return &mockPermTransport{id: id, permissionResult: transport.PermissionDenied}
	}
	RegisterPermissionTool(r, ps, getTransport)

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-tp", Type: "stdio"})
	args, _ := json.Marshal(map[string]string{"tool_name": "some_tool", "reason": "need it"})
	result, err := r.Execute(ctx, types.ToolCall{
		ID: "call-5", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "denied") {
		t.Errorf("expected 'denied' in response, got: %s", result.Content)
	}
}

func TestRequestPermissionOnce(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	getTransport := func(id string) transport.IO {
		return &mockPermTransport{id: id, permissionResult: transport.PermissionOnce}
	}
	RegisterPermissionTool(r, ps, getTransport)

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-tp", Type: "stdio"})
	args, _ := json.Marshal(map[string]string{"tool_name": "some_tool", "reason": "need it"})
	result, err := r.Execute(ctx, types.ToolCall{
		ID: "call-6", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Permission granted") {
		t.Errorf("expected 'Permission granted' in response, got: %s", result.Content)
	}
}

func TestRequestPermissionAlways(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	getTransport := func(id string) transport.IO {
		return &mockPermTransport{id: id, permissionResult: transport.PermissionAlways}
	}
	RegisterPermissionTool(r, ps, getTransport)

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-tp", Type: "stdio"})
	args, _ := json.Marshal(map[string]string{"tool_name": "some_tool", "reason": "need it"})
	result, err := r.Execute(ctx, types.ToolCall{
		ID: "call-7", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "saved to rules") {
		t.Errorf("expected 'saved to rules' in response, got: %s", result.Content)
	}
}

func TestRequestPermissionNoTransportCtx(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	getTransport := func(id string) transport.IO {
		return &mockPermTransport{id: id, permissionResult: transport.PermissionOnce}
	}
	RegisterPermissionTool(r, ps, getTransport)

	args, _ := json.Marshal(map[string]string{"tool_name": "some_tool", "reason": "need it"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-8", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError when no transport context")
	}
	if !strings.Contains(result.Content, "no transport context") {
		t.Errorf("expected 'no transport context' error, got: %s", result.Content)
	}
}

func TestRequestPermissionTransportNotFound(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore("")
	getTransport := func(id string) transport.IO {
		return nil
	}
	RegisterPermissionTool(r, ps, getTransport)

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "unknown-tp", Type: "stdio"})
	args, _ := json.Marshal(map[string]string{"tool_name": "some_tool", "reason": "need it"})
	result, err := r.Execute(ctx, types.ToolCall{
		ID: "call-9", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError when transport not found")
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' error, got: %s", result.Content)
	}
}

func TestEmitEventNilBus(t *testing.T) {
	r := NewRegistry()
	RegisterEmitEventTool(r, nil)

	args, _ := json.Marshal(map[string]string{
		"name": "test-event",
		"desc": "nil bus test",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "emit_event", Arguments: string(args),
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
}

func TestRequestPermissionWithArguments(t *testing.T) {
	r := NewRegistry()
	ps := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	getTransport := func(id string) transport.IO {
		return &mockPermTransport{id: id, permissionResult: transport.PermissionAlways}
	}
	RegisterPermissionTool(r, ps, getTransport)

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-tp", Type: "stdio"})
	args, _ := json.Marshal(map[string]any{
		"tool_name": "some_tool",
		"reason":    "need it",
		"arguments": map[string]string{"key": "value"},
	})
	result, err := r.Execute(ctx, types.ToolCall{
		ID: "call-11", Name: "request_permission", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "saved to rules") {
		t.Errorf("expected 'saved to rules' in response, got: %s", result.Content)
	}
}

func TestEmitEventEventReceived(t *testing.T) {
	r := NewRegistry()
	bus := event.NewBus()
	RegisterEmitEventTool(r, bus)

	var receivedName string
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		if name, ok := e.Payload["name"].(string); ok {
			receivedName = name
		}
	})

	args, _ := json.Marshal(map[string]string{
		"name": "my-custom-event",
		"desc": "custom description",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-12", Name: "emit_event", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if receivedName != "my-custom-event" {
		t.Errorf("expected 'my-custom-event', got %q", receivedName)
	}
}
