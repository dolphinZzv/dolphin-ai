package mcp

import (
	"context"
	"fmt"
	"testing"

	"dolphin/internal/transport"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

func TestPandaSource_List_WithPandaContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	tools, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "MESSAGE" {
		t.Fatalf("expected MESSAGE, got %s", tools[0].Name)
	}
	if tools[0].Schema == nil {
		t.Fatal("expected MESSAGE schema")
	}
}

func TestPandaSource_List_WithoutPandaContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	tools, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tools != nil {
		t.Fatal("expected nil tools when not panda transport")
	}
}

func TestPandaSource_List_WrongTransport(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
	tools, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tools != nil {
		t.Fatal("expected nil tools for non-panda transport")
	}
}

func TestPandaSource_Execute_NoContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	_, err := s.Execute(context.Background(), types.ToolCall{Name: "MESSAGE"})
	if err == nil {
		t.Fatal("expected error when no panda context")
	}
}

func TestPandaSource_Execute_WrongContext(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
	_, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE"})
	if err == nil {
		t.Fatal("expected error when wrong transport context")
	}
}

func TestPandaSource_Execute_UnknownTool(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	_, err := s.Execute(ctx, types.ToolCall{Name: "UNKNOWN"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestPandaSource_NilLogger(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, nil)
	if s == nil {
		t.Fatal("expected non-nil source")
	}
}

// --- MESSAGE tests ---

func TestMessage_InvalidArgs(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{invalid}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid args")
	}
}

func TestMessage_EmptyContent(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":""}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for empty content")
	}
}

func TestMessage_Success(t *testing.T) {
	var got string
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error {
		got = text
		return nil
	}, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hello from mcp"}`})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if got != "hello from mcp" {
		t.Fatalf("expected 'hello from mcp', got '%s'", got)
	}
}

func TestMessage_WriteError(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error {
		return fmt.Errorf("write failed")
	}, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())

	ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "panda"})
	result, err := s.Execute(ctx, types.ToolCall{Name: "MESSAGE", Arguments: `{"content":"hello"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for write failure")
	}
}

// TestNewFileUploadSource_Source tests that the returned source implements tool.Executor.
func TestNewFileUploadSource_Source(t *testing.T) {
	s := NewFileUploadSource("http://localhost:8080", func() string { return "tok" }, func(ctx context.Context, text string) error { return nil }, func(ctx context.Context, text string, contentType int) error { return nil }, zap.NewNop())
	if _, ok := s.(interface {
		List(ctx context.Context) ([]types.ToolDef, error)
		Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error)
	}); !ok {
		t.Fatal("NewFileUploadSource should return an executor")
	}
}
