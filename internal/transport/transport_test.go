package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"dolphin/internal/types"
)

func TestStdio_Read(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	os.Stdin = r

	s := NewStdio("test", "Dolphin")
	s.rl = nil // force fallback reader in test

	go func() {
		w.WriteString("hello\n")
		w.Close()
	}()

	result, err := s.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Fatalf("expected 'hello', got '%s'", result)
	}
}

func TestStdio_Write(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	os.Stdout = w

	s := NewStdio("test", "Dolphin")

	err = s.Write(context.Background(), "test output")
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, r)
	if buf.String() != "test output" {
		t.Fatalf("expected 'test output', got '%s'", buf.String())
	}
}

func TestStdio_Flush(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	t.Cleanup(func() { os.Stdout = oldStdout })
	os.Stdout = w

	s := NewStdio("test", "Dolphin")
	err = s.Flush()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	buf := new(bytes.Buffer)
	_, _ = io.Copy(buf, r)
	if buf.String() != "\n" {
		t.Fatalf("expected newline, got '%s'", buf.String())
	}
}

func TestStdio_Close(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	s := NewStdio("test", "Dolphin")
	err := s.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestStdio_ID(t *testing.T) {
	s := NewStdio("test", "Dolphin")
	if s.ID() != "stdio" {
		t.Fatalf("expected 'stdio', got '%s'", s.ID())
	}
}

func TestStdio_Capability(t *testing.T) {
	s := NewStdio("test", "Dolphin")
	c := s.Capability()
	if !c.Interactive {
		t.Fatal("expected Interactive=true")
	}
	if !c.Streamable {
		t.Fatal("expected Streamable=true")
	}
	if !c.NestRead {
		t.Fatal("expected NestRead=true")
	}
}

func TestStdio_ReadContextCancelled(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	os.Stdin = r

	s := NewStdio("test", "Dolphin")
	s.rl = nil // force fallback reader in test

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = s.Read(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	w.Close()
}

func TestNullTransport_Read(t *testing.T) {
	n := NewNullTransport("null")
	n.readBuf = []string{"line1", "line2"}

	r1, err := n.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r1 != "line1" {
		t.Fatalf("expected 'line1', got '%s'", r1)
	}

	r2, err := n.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r2 != "line2" {
		t.Fatalf("expected 'line2', got '%s'", r2)
	}

	_, err = n.Read(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestNullTransport_Write(t *testing.T) {
	n := NewNullTransport("null")
	err := n.Write(context.Background(), "anything")
	if err != nil {
		t.Fatal(err)
	}
}

func TestNullTransport_Flush(t *testing.T) {
	n := NewNullTransport("null")
	err := n.Flush()
	if err != nil {
		t.Fatal(err)
	}
}

func TestNullTransport_Close(t *testing.T) {
	n := NewNullTransport("null")
	err := n.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestNullTransport_ID(t *testing.T) {
	n := NewNullTransport("test-id")
	if n.ID() != "test-id" {
		t.Fatalf("expected 'test-id', got '%s'", n.ID())
	}
}

func TestNullTransport_Capability(t *testing.T) {
	n := NewNullTransport("null")
	c := n.Capability()
	if c.Interactive {
		t.Fatal("expected Interactive=false")
	}
	if c.Streamable {
		t.Fatal("expected Streamable=false")
	}
	if c.NestRead {
		t.Fatal("expected NestRead=false")
	}
}

func TestNullTransport_RequestPermission(t *testing.T) {
	n := NewNullTransport("null")
	result, err := n.RequestPermission(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result != PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %d", result)
	}
}

func TestStdio_RequestPermission_Denied(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	})
	os.Stdin = stdinR
	os.Stdout = stdoutW

	s := NewStdio("test", "Dolphin")
	s.rl = nil

	go func() {
		stdinW.WriteString("3\n")
		stdinW.Close()
		stdoutW.Close()
	}()

	result, err := s.RequestPermission(context.Background(), "allow?")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result != PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %d", result)
	}
	// Drain stdout to avoid pipe deadlock.
	go io.Copy(io.Discard, stdoutR)
}

func TestStdio_RequestPermission_Once(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	})
	os.Stdin = stdinR
	os.Stdout = stdoutW

	s := NewStdio("test", "Dolphin")
	s.rl = nil

	go func() {
		stdinW.WriteString("1\n")
		stdinW.Close()
		stdoutW.Close()
	}()

	result, err := s.RequestPermission(context.Background(), "allow?")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result != PermissionOnce {
		t.Fatalf("expected PermissionOnce, got %d", result)
	}
	go io.Copy(io.Discard, stdoutR)
}

func TestStdio_RequestPermission_Always(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	})
	os.Stdin = stdinR
	os.Stdout = stdoutW

	s := NewStdio("test", "Dolphin")
	s.rl = nil

	go func() {
		stdinW.WriteString("2\n")
		stdinW.Close()
		stdoutW.Close()
	}()

	result, err := s.RequestPermission(context.Background(), "allow?")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result != PermissionAlways {
		t.Fatalf("expected PermissionAlways, got %d", result)
	}
	go io.Copy(io.Discard, stdoutR)
}

func TestRegistry_RegisterAndBuild(t *testing.T) {
	r := NewRegistry()

	r.Register("test", func(ctx context.Context, cfg map[string]any) (IO, error) {
		return NewNullTransport("built"), nil
	})

	io, err := r.Build(context.Background(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if io.ID() != "built" {
		t.Fatalf("expected 'built', got '%s'", io.ID())
	}
}

func TestRegistry_BuildUnknown(t *testing.T) {
	r := NewRegistry()

	_, err := r.Build(context.Background(), "unknown", nil)
	if err == nil {
		t.Fatal("expected error for unknown transport type")
	}
	if !strings.Contains(err.Error(), "unknown transport type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGlobalRegisterAndBuild(t *testing.T) {
	Register("test_global", func(ctx context.Context, cfg map[string]any) (IO, error) {
		return NewNullTransport("global"), nil
	})

	io, err := Build(context.Background(), "test_global", nil)
	if err != nil {
		t.Fatal(err)
	}
	if io.ID() != "global" {
		t.Fatalf("expected 'global', got '%s'", io.ID())
	}
}

func TestGlobalBuildUnknown(t *testing.T) {
	_, err := Build(context.Background(), "__nonexistent__", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWithInfoAndGetInfo(t *testing.T) {
	info := &Info{
		ID:       "sess-1",
		Type:     "stdio",
		ClientIP: "127.0.0.1",
	}

	ctx := WithInfo(context.Background(), info)
	got := GetInfo(ctx)

	if got == nil {
		t.Fatal("expected info, got nil")
	}
	if got.ID != "sess-1" {
		t.Fatalf("expected 'sess-1', got '%s'", got.ID)
	}
	if got.Type != "stdio" {
		t.Fatalf("expected 'stdio', got '%s'", got.Type)
	}
	if got.ClientIP != "127.0.0.1" {
		t.Fatalf("expected '127.0.0.1', got '%s'", got.ClientIP)
	}
}

func TestGetInfoEmptyContext(t *testing.T) {
	got := GetInfo(context.Background())
	if got != nil {
		t.Fatal("expected nil from context without info")
	}
}

func TestGetInfoNilContext(t *testing.T) {
	got := GetInfo(nil)
	if got != nil {
		t.Fatal("expected nil from nil context")
	}
}

func TestStdio_BuiltinRegistration(t *testing.T) {
	// The stdio transport is registered in init(), so we should be able to build it.
	// This also tests the global Registry used by Register/Build.
	io, err := Build(context.Background(), "stdio", nil)
	if err != nil {
		t.Fatal(err)
	}
	if io.ID() != "stdio" {
		t.Fatalf("expected 'stdio', got '%s'", io.ID())
	}
}

func TestSessionHolder_WriteThinking(t *testing.T) {
	h := NewSessionHolder(nil)
	if err := h.WriteThinking(context.Background(), "thinking..."); err != nil {
		t.Errorf("WriteThinking should return nil, got %v", err)
	}
}

func TestSessionHolder_WriteToolCall(t *testing.T) {
	h := NewSessionHolder(nil)
	if err := h.WriteToolCall(context.Background(), types.ToolCall{Name: "test"}); err != nil {
		t.Errorf("WriteToolCall should return nil, got %v", err)
	}
}

func TestSessionHolder_WriteToolResult(t *testing.T) {
	h := NewSessionHolder(nil)
	if err := h.WriteToolResult(context.Background(), types.ToolResult{Content: "done"}); err != nil {
		t.Errorf("WriteToolResult should return nil, got %v", err)
	}
}
