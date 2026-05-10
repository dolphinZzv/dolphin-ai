package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func TestRegistryMostUsedToolsPriority(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "low", priority: 10})
	r.Register(&testTool{name: "high", priority: 1000})
	r.Register(&testTool{name: "default"}) // uses DefaultPriority (100)

	top := r.MostUsedTools(3)
	if len(top) != 3 {
		t.Fatalf("got %d, want 3", len(top))
	}
	// low(10) < default(100) < high(1000)
	if top[0].Name != "low" {
		t.Errorf("expected low first (priority 10), got %s", top[0].Name)
	}
	if top[1].Name != "default" {
		t.Errorf("expected default second (priority 100), got %s", top[1].Name)
	}
	if top[2].Name != "high" {
		t.Errorf("expected high third (priority 1000), got %s", top[2].Name)
	}
}

func TestRegistryMostUsedToolsPriorityWithinSameLevel(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "less-used", priority: 10})
	r.Register(&testTool{name: "more-used", priority: 10})

	// Call more-used twice
	r.Execute(context.Background(), "less-used", json.RawMessage(`{}`))
	r.Execute(context.Background(), "more-used", json.RawMessage(`{}`))
	r.Execute(context.Background(), "more-used", json.RawMessage(`{}`))

	top := r.MostUsedTools(2)
	if len(top) != 2 {
		t.Fatalf("got %d, want 2", len(top))
	}
	// Same priority, so call count should decide
	if top[0].Name != "more-used" {
		t.Errorf("expected more-used first (called 2x), got %s", top[0].Name)
	}
	if top[1].Name != "less-used" {
		t.Errorf("expected less-used second (called 1x), got %s", top[1].Name)
	}
}

func TestRegistryMostUsedToolsRegistrationOrderTiebreaker(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	// Same priority, same call count — registration order decides
	r.Register(&testTool{name: "beta", priority: 10})
	r.Register(&testTool{name: "alpha", priority: 10})

	top := r.MostUsedTools(2)
	if len(top) != 2 {
		t.Fatalf("got %d, want 2", len(top))
	}
	// beta registered before alpha
	if top[0].Name != "beta" {
		t.Errorf("expected beta first (registered first), got %s", top[0].Name)
	}
}

func TestRegistryOrderIndex(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	if idx := r.orderIndex("nonexistent"); idx != 0 {
		t.Errorf("expected 0 for empty registry, got %d", idx)
	}

	r.Register(&testTool{name: "first"})
	r.Register(&testTool{name: "second"})
	r.Register(&testTool{name: "third"})

	if idx := r.orderIndex("first"); idx != 0 {
		t.Errorf("expected 0 for first, got %d", idx)
	}
	if idx := r.orderIndex("second"); idx != 1 {
		t.Errorf("expected 1 for second, got %d", idx)
	}
	if idx := r.orderIndex("third"); idx != 2 {
		t.Errorf("expected 2 for third, got %d", idx)
	}
	if idx := r.orderIndex("unknown"); idx != 3 {
		t.Errorf("expected 3 for unknown (len(order)), got %d", idx)
	}
}

func TestRegistryFilteredViewPreservesOrder(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "a"})
	r.Register(&testTool{name: "b"})
	r.Register(&testTool{name: "c"})

	fv := r.FilteredView(nil)
	if fv.orderIndex("a") != 0 || fv.orderIndex("b") != 1 || fv.orderIndex("c") != 2 {
		t.Error("FilteredView should preserve order")
	}
}

func TestRegistryClonePreservesOrder(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.Register(&testTool{name: "x"})
	r.Register(&testTool{name: "y"})

	clone := r.Clone()
	if clone.orderIndex("x") != 0 || clone.orderIndex("y") != 1 {
		t.Error("Clone should preserve order")
	}
}

// testTool implements Tool for testing.
type testTool struct {
	name     string
	priority int
}

func (t *testTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.name,
		Description: "test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Priority:    t.priority,
	}
}

func (t *testTool) Execute(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	return &ToolResult{Content: "ok"}, nil
}

func TestAverageDurationMsZero(t *testing.T) {
	s := &ToolStats{}
	if avg := s.AverageDurationMs(); avg != 0 {
		t.Errorf("expected 0, got %f", avg)
	}
}

func TestAverageDurationMs(t *testing.T) {
	s := &ToolStats{
		CallCount:     2,
		TotalDuration: 100 * time.Millisecond,
	}
	if avg := s.AverageDurationMs(); avg != 50 {
		t.Errorf("expected 50, got %f", avg)
	}
}
