package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"dolphin/internal/config"
)

// roundTripperFunc adapts a function to http.RoundTripper for test HTTP mocking.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// ---- parseQueries ----

func TestParseQueries_SingleString(t *testing.T) {
	raw := json.RawMessage(`"golang tutorial"`)
	queries, err := parseQueries(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queries) != 1 || queries[0] != "golang tutorial" {
		t.Fatalf("expected [golang tutorial], got %v", queries)
	}
}

func TestParseQueries_Array(t *testing.T) {
	raw := json.RawMessage(`["go 1.22 release", "golang generics", "go testing"]`)
	queries, err := parseQueries(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queries) != 3 {
		t.Fatalf("expected 3 queries, got %d: %v", len(queries), queries)
	}
}

func TestParseQueries_ArrayWithEmpty(t *testing.T) {
	raw := json.RawMessage(`["go 1.22", "", "golang", ""]`)
	queries, err := parseQueries(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 non-empty queries, got %d: %v", len(queries), queries)
	}
}

func TestParseQueries_EmptyString(t *testing.T) {
	raw := json.RawMessage(`""`)
	queries, err := parseQueries(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queries != nil {
		t.Fatalf("expected nil for empty string, got %v", queries)
	}
}

func TestParseQueries_InvalidType(t *testing.T) {
	raw := json.RawMessage(`42`)
	_, err := parseQueries(raw)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

// ---- Definition ----

func TestDefinition_Name(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	def := tool.Definition()
	if def.Name != "web_search" {
		t.Fatalf("expected name 'web_search', got %q", def.Name)
	}
	if def.Source != "built-in" {
		t.Fatalf("expected source 'built-in', got %q", def.Source)
	}
}

func TestDefinition_InputSchema_HasOneOf(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	def := tool.Definition()

	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	queryProp, ok := props["query"].(map[string]any)
	if !ok {
		t.Fatal("schema missing query property")
	}
	if _, ok := queryProp["oneOf"]; !ok {
		t.Fatal("query property missing oneOf")
	}
}

// ---- Shared error cases ----

func TestExecute_EmptyQuery_ReturnsError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)

	input, _ := json.Marshal(map[string]any{"query": ""})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for empty query, got: %s", result.Content)
	}
}

func TestExecute_InvalidInput_ReturnsError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)

	input, _ := json.Marshal(map[string]any{"query": 42})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for invalid input, got: %s", result.Content)
	}
}

func TestExecute_MissingQuery_ReturnsError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)

	input, _ := json.Marshal(map[string]any{})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for missing query, got: %s", result.Content)
	}
}

func TestExecute_ContextCancelled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input, _ := json.Marshal(map[string]any{"query": "test"})
	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
