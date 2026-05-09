package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"dolphinzZ/internal/config"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "echo"})

	tool, ok := r.Get("echo")
	if !ok {
		t.Fatal("expected to find tool 'echo'")
	}
	if tool.Definition().Name != "echo" {
		t.Errorf("tool name = %q", tool.Definition().Name)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent tool")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})

	defs := r.List()
	if len(defs) != 2 {
		t.Fatalf("got %d definitions, want 2", len(defs))
	}
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "echo"})

	result, err := r.Execute(context.Background(), "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content != "ok" {
		t.Errorf("result = %q, want ok", result.Content)
	}
}

func TestRegistryExecuteNotFound(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	_, err := r.Execute(context.Background(), "missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestRegistryExecuteWithStats(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "echo"})

	// Execute twice
	r.Execute(context.Background(), "echo", json.RawMessage(`{}`))
	r.Execute(context.Background(), "echo", json.RawMessage(`{}`))

	stats := r.ToolStats()
	s, ok := stats["echo"]
	if !ok {
		t.Fatal("expected stats for echo")
	}
	if s.CallCount != 2 {
		t.Errorf("call count = %d, want 2", s.CallCount)
	}
	if s.LastCalledAt.IsZero() {
		t.Error("expected LastCalledAt to be set")
	}
}

func TestRegistryMostUsedTools(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})

	// Execute "a" twice, "b" once
	r.Execute(context.Background(), "a", json.RawMessage(`{}`))
	r.Execute(context.Background(), "a", json.RawMessage(`{}`))
	r.Execute(context.Background(), "b", json.RawMessage(`{}`))

	top := r.MostUsedTools(2)
	if len(top) != 2 {
		t.Fatalf("got %d, want 2", len(top))
	}
	// "a" should be first (most used)
	if top[0].Name != "a" {
		t.Errorf("expected a first, got %s", top[0].Name)
	}
}

func TestRegistryMostUsedToolsLimit(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})
	r.Register(&testTool{name: "c"})

	top := r.MostUsedTools(2)
	if len(top) != 2 {
		t.Errorf("got %d, want 2", len(top))
	}
}

func TestRegistrySearchTools(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "shell"})
	r.Register(&testTool{name: "cdp"})

	results := r.SearchTools("shell")
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "shell" {
		t.Errorf("got %s, want shell", results[0].Name)
	}
}

func TestRegistrySearchToolsNoMatch(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "shell"})

	results := r.SearchTools("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// testTool implements Tool for testing.
type testTool struct {
	name string
}

func (t *testTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.name,
		Description: "test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func (t *testTool) Execute(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	return &ToolResult{Content: "ok"}, nil
}
