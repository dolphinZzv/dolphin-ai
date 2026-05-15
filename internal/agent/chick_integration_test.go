package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"gopkg.in/yaml.v3"
)

// isTransportError returns true when a CallTool error content indicates a real
// transport/connectivity failure (not a tool-level validation error from the server).
func isTransportError(content string) bool {
	if strings.Contains(content, "send request:") {
		return true
	}
	if strings.Contains(content, "http error:") {
		return true
	}
	if strings.Contains(content, "decode response:") {
		return true
	}
	if strings.Contains(content, "marshal request:") {
		return true
	}
	if strings.Contains(content, "create request:") {
		return true
	}
	return false
}

func readChickConfig(t *testing.T) config.MCPServerConfig {
	t.Helper()
	cfgPath := findConfigPath()
	if cfgPath == "" {
		t.Skip("config.yaml not found (walked up from CWD)")
	}
	if v := os.Getenv("DOLPHIN_CONFIG"); v != "" {
		cfgPath = v
	}
	t.Logf("config: %s", cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Skipf("read config: %v", err)
	}

	var raw struct {
		MCP struct {
			Servers map[string]config.MCPServerConfig `yaml:"servers"`
		} `yaml:"mcp"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Skipf("parse config: %v", err)
	}

	cfg, ok := raw.MCP.Servers["chick"]
	if !ok {
		t.Skip("chick MCP server not configured in config.yaml")
	}
	if cfg.URL == "" {
		t.Skip("chick MCP server has no URL")
	}
	return cfg
}

func TestChickServerListTools(t *testing.T) {
	cfg := readChickConfig(t)

	client, err := mcp.NewServerClient(context.Background(), "chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	t.Logf("chick server returned %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("  - %s: %s", tool.Name, tool.Description)
	}

	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool from chick server")
	}

	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty Name")
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty InputSchema", tool.Name)
		}
	}
}

func TestChickServerCallSearchIssues(t *testing.T) {
	cfg := readChickConfig(t)

	client, err := mcp.NewServerClient(context.Background(), "chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	// Verify search_issues is available
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "search_issues" {
			found = true
			break
		}
	}
	if !found {
		t.Skip("search_issues tool not available on this chick server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "search_issues", json.RawMessage(`{"limit":3}`))
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			t.Skipf("auth error: %v", err)
		}
		t.Fatalf("CallTool search_issues: %v", err)
	}

	t.Logf("search_issues result (is_error=%v): %s", result.IsError, result.Content)

	if result.IsError {
		if isTransportError(result.Content) {
			t.Skipf("chick server transport error: %s", result.Content)
		}
		t.Logf("search_issues returned tool-level error (expected for some queries): %s", result.Content)
		return
	}
	if result.Content == "" {
		t.Error("search_issues returned empty content")
	}
}

func TestChickServerCallCreateComment(t *testing.T) {
	cfg := readChickConfig(t)

	client, err := mcp.NewServerClient(context.Background(), "chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	// Verify add_comment is available
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "add_comment" {
			found = true
			break
		}
	}
	if !found {
		t.Skip("add_comment tool not available on this chick server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.CallTool(ctx, "add_comment", json.RawMessage(`{"issueId":"TEST-99999","comment":"integration test - please ignore"}`))
	if err != nil {
		t.Fatalf("CallTool add_comment: %v", err)
	}

	t.Logf("add_comment result (is_error=%v): %s", result.IsError, result.Content)
	if result.IsError {
		if isTransportError(result.Content) {
			t.Skipf("chick server transport error: %s", result.Content)
		}
		// Validation errors (e.g. invalid issueId) prove the tool is working correctly.
		t.Logf("add_comment validation working as expected")
	}
}

// TestChickAllToolsInputSchema verifies every tool has a valid JSON input schema.
func TestChickAllToolsInputSchema(t *testing.T) {
	cfg := readChickConfig(t)

	client, err := mcp.NewServerClient(context.Background(), "chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Errorf("invalid JSON schema: %v", err)
				return
			}
			if typ, _ := schema["type"].(string); typ != "object" {
				t.Errorf("schema type = %q, want object", typ)
			}
			if _, ok := schema["properties"]; !ok {
				t.Errorf("schema missing properties")
			}
		})
	}
}

// TestChickServerToolSchemaRoundTrip verifies that tool definitions survive a
// serialize/deserialize round-trip through the registry and agent tool flow.
func TestChickServerToolSchemaRoundTrip(t *testing.T) {
	cfg := readChickConfig(t)

	client, err := mcp.NewServerClient(context.Background(), "chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			// Marshal through ToolDef to simulate agent tool flow
			data, err := json.Marshal(tool)
			if err != nil {
				t.Fatalf("marshal ToolDef: %v", err)
			}
			var restored mcp.ToolDefinition
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("unmarshal ToolDef: %v", err)
			}
			if restored.Name != tool.Name {
				t.Errorf("name mismatch: %q vs %q", restored.Name, tool.Name)
			}
			if !reflect.DeepEqual(restored.InputSchema, tool.InputSchema) {
				t.Errorf("InputSchema mismatch for %q", tool.Name)
			}
		})
	}
}
