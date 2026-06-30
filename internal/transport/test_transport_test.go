package transport

import (
	"context"
	"testing"
)

func TestTestTransport_ID(t *testing.T) {
	tt := NewTestTransport("test-id")
	if tt.ID() != "test-id" {
		t.Errorf("expected test-id, got %s", tt.ID())
	}
}

func TestTestTransport_Context(t *testing.T) {
	tt := NewTestTransport("test")
	if tt.Context() != "" {
		t.Errorf("expected empty context, got %s", tt.Context())
	}
}

func TestTestTransport_Tools(t *testing.T) {
	tt := NewTestTransport("test")
	if tt.Tools() != nil {
		t.Error("expected nil tools")
	}
}

func TestTestTransport_Start(t *testing.T) {
	tt := NewTestTransport("test")
	if err := tt.Start(context.Background()); err != nil {
		t.Errorf("Start failed: %v", err)
	}
}

func TestTestTransport_Read(t *testing.T) {
	tt := NewTestTransport("test")
	go func() {
		tt.SendInput("hello")
	}()
	msg, err := tt.Read(context.Background())
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if msg.Text != "hello" {
		t.Errorf("expected hello, got %s", msg.Text)
	}
}

func TestTestTransport_ReadContextCancel(t *testing.T) {
	tt := NewTestTransport("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tt.Read(ctx)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestTestTransport_ReadEOF(t *testing.T) {
	tt := NewTestTransport("test")
	tt.Close()
	_, err := tt.Read(context.Background())
	if err == nil {
		t.Error("expected EOF after close")
	}
}

func TestTestTransport_Write(t *testing.T) {
	tt := NewTestTransport("test")
	tt.Write(context.Background(), "hello")
	tt.Write(context.Background(), " world")
	if tt.Output() != "hello world" {
		t.Errorf("expected 'hello world', got %q", tt.Output())
	}
}

func TestTestTransport_Flush(t *testing.T) {
	tt := NewTestTransport("test")
	tt.Write(context.Background(), "hello")
	tt.Flush()
	if tt.Output() != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", tt.Output())
	}
}

func TestTestTransport_Contains(t *testing.T) {
	tt := NewTestTransport("test")
	tt.Write(context.Background(), "hello world")
	if !tt.Contains("world") {
		t.Error("expected to contain 'world'")
	}
	if tt.Contains("nope") {
		t.Error("should not contain 'nope'")
	}
}

func TestTestTransport_Close(t *testing.T) {
	tt := NewTestTransport("test")
	if err := tt.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	// Double close should not panic.
	if err := tt.Close(); err != nil {
		t.Errorf("Double close failed: %v", err)
	}
}

func TestTestTransport_RequestPermission(t *testing.T) {
	tt := NewTestTransport("test")
	result, err := tt.RequestPermission(context.Background(), "approve?")
	if err != nil {
		t.Errorf("RequestPermission failed: %v", err)
	}
	if result != PermissionOnce {
		t.Errorf("expected PermissionOnce, got %v", result)
	}
	if !tt.Contains("[PERMISSION:approve?]") {
		t.Error("expected permission message in output")
	}
}

func TestTestTransport_Capability(t *testing.T) {
	tt := NewTestTransport("test")
	c := tt.Capability()
	if !c.Interactive {
		t.Error("expected Interactive to be true")
	}
	if !c.Streamable {
		t.Error("expected Streamable to be true")
	}
}

func TestTestTransport_ImplementsIO(t *testing.T) {
	var _ IO = NewTestTransport("test")
}
