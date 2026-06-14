package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/types"
)

func TestBuiltinMCPHandlers(t *testing.T) {
	handlers := BuiltinMCPHandlers(nil)
	if len(handlers) == 0 {
		t.Fatal("expected at least 1 builtin handler")
	}
	for name := range handlers {
		if name == "" {
			t.Fatal("empty handler name")
		}
	}
}

func TestBuiltinMCPDescriptions(t *testing.T) {
	descs := BuiltinMCPDescriptions()
	if len(descs) == 0 {
		t.Fatal("expected at least 1 description")
	}
}

func TestBuiltinMCPSchemas(t *testing.T) {
	schemas := BuiltinMCPSchemas()
	if len(schemas) == 0 {
		t.Fatal("expected at least 1 schema")
	}
}

func TestHandleShell_Success(t *testing.T) {
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("expected 'hello' in output, got: %s", result.Content)
	}
}

func TestHandleShell_InvalidArgs(t *testing.T) {
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid args")
	}
	if !strings.Contains(result.Content, "invalid args") {
		t.Fatalf("expected 'invalid args' in error, got: %s", result.Content)
	}
}

func TestHandleShell_EmptyCommand(t *testing.T) {
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(context.Background(), json.RawMessage(`{"command":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for empty command")
	}
}

func TestShellHandler_WithBinDirs(t *testing.T) {
	h := BuiltinMCPHandlers([]string{"/nonexistent/path"})["shell"]
	result, err := h(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("expected 'hello' in output, got: %s", result.Content)
	}
}

func TestBuiltinMCPHandlers_Consistency(t *testing.T) {
	// Ensure handler, description, and schema counts match.
	handlers := BuiltinMCPHandlers(nil)
	descs := BuiltinMCPDescriptions()
	schemas := BuiltinMCPSchemas()

	for name := range handlers {
		if _, ok := descs[name]; !ok {
			t.Fatalf("missing description for builtin %q", name)
		}
		if _, ok := schemas[name]; !ok {
			t.Fatalf("missing schema for builtin %q", name)
		}
	}
}

func TestMetaHandler_MCPSearch(t *testing.T) {
	catalog := NewCatalog([]CatalogEntry{
		{Name: "test-server", Description: "A test server", Tags: []string{"test"}},
	})
	registry := NewRegistry()
	handlers := MetaHandler(catalog, registry)

	handler, ok := handlers["mcp_search"]
	if !ok {
		t.Fatal("expected mcp_search handler")
	}

	result, err := handler.Handler(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(result.Content, "test-server") {
		t.Fatalf("expected 'test-server' in results, got: %s", result.Content)
	}
}

func TestMetaHandler_MCPSearchInvalidArgs(t *testing.T) {
	catalog := NewCatalog(nil)
	registry := NewRegistry()
	handlers := MetaHandler(catalog, registry)

	handler := handlers["mcp_search"]
	result, err := handler.Handler(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid args")
	}
}

func TestMetaHandler_MCPLoadInvalidArgs(t *testing.T) {
	catalog := NewCatalog(nil)
	registry := NewRegistry()
	handlers := MetaHandler(catalog, registry)

	handler := handlers["mcp_load"]
	result, err := handler.Handler(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid args")
	}
}

func TestRegisterBuiltin_NilSchema(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterBuiltin("nil_schema", "desc", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: "ok"}, nil
	})
	defs, err := registry.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(defs))
	}
	if defs[0].Name != "nil_schema" {
		t.Fatalf("expected 'nil_schema', got '%s'", defs[0].Name)
	}
}

func TestExecuteWithTimeout_CancelledContext(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterBuiltin("slow", "desc", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ExecuteWithTimeout(ctx, registry, types.ToolCall{Name: "slow"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cancelled context")
	}
}
