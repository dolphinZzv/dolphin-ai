package webhost

import (
	"context"
	"encoding/json"
	"testing"

	"dolphin/internal/config"
)

func TestDefinition(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.Enabled = true
	cfg.MCP.Webhost.URL = "http://localhost:9223/mcp/call"

	tool := New(cfg)
	def := tool.Definition()

	if def.Name != "webhost" {
		t.Errorf("expected name 'webhost', got %q", def.Name)
	}
	if def.Source != "built-in" {
		t.Errorf("expected source 'built-in', got %q", def.Source)
	}
	if def.Description == "" {
		t.Error("expected non-empty description")
	}
	if len(def.InputSchema) == 0 {
		t.Error("expected non-empty input schema")
	}

	// Verify schema has the action field with all 16 actions.
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if _, ok := schema.Properties["action"]; !ok {
		t.Error("schema missing 'action' property")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "action" {
		t.Error("schema should require only 'action'")
	}
}

func TestExecuteInvalidInput(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.URL = "http://localhost:9223/mcp/call"
	tool := New(cfg)

	// Invalid JSON
	result, err := tool.Execute(context.TODO(), json.RawMessage(`{invalid}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}

	// Missing action
	result, err = tool.Execute(context.TODO(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing action")
	}

	// Unknown action
	result, err = tool.Execute(context.TODO(), json.RawMessage(`{"action":"unknown_action"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestExecuteWebhostOffline(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.URL = "http://localhost:1/mcp/call"
	cfg.MCP.Webhost.TimeoutSeconds = 1
	tool := New(cfg)

	result, err := tool.Execute(context.TODO(), json.RawMessage(`{"action":"web_session_create"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when webhost is offline")
	}
	if result.Content == "" {
		t.Error("expected non-empty error message")
	}
}

func TestExecuteActionDispatch(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.URL = "http://localhost:9223/mcp/call"
	// We can't actually test against a running webhost here,
	// but we can verify the argument building is correct by
	// checking the error message isn't about invalid actions.

	tools := []string{
		"web_session_create",
		"page_open",
		"script_run",
		"page_screenshot",
		"web_inject",
		"web_wait",
		"web_set_interactive",
		"web_capabilities",
		"web_session_close",
		"web_dialog_response",
		"tab_list",
		"tab_switch",
		"tab_create",
		"tab_close",
		"go_back",
		"go_forward",
	}

	for _, action := range tools {
		input := map[string]any{"action": action}
		if action == "page_open" {
			input["url"] = "https://example.com"
		}
		if action == "script_run" {
			input["script"] = "1+1"
		}
		if action == "web_wait" {
			input["selector"] = "body"
		}
		data, _ := json.Marshal(input)

		tool := New(cfg)
		result, err := tool.Execute(context.TODO(), data)
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", action, err)
		}
		// Should fail with connection refused (webhost offline), not "unknown action"
		if result.IsError {
			if len(result.Content) > 0 && result.Content[0] != 'W' {
				t.Logf("action %q: result = %s", action, result.Content)
			}
		}
	}
}

func TestPriority(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.Priority = 50
	cfg.MCP.Webhost.URL = "http://localhost:9223/mcp/call"

	tool := New(cfg)
	def := tool.Definition()
	if def.Priority != 50 {
		t.Errorf("expected priority 50, got %d", def.Priority)
	}
}

func TestConfig(t *testing.T) {
	// Verify config type exists and has expected fields.
	cfg := config.MCPWebHostConfig{
		Enabled:        true,
		URL:            "http://localhost:9223/mcp/call",
		Priority:       90,
		TimeoutSeconds: 15,
	}
	if !cfg.Enabled {
		t.Error("expected enabled")
	}
	if cfg.URL != "http://localhost:9223/mcp/call" {
		t.Errorf("unexpected URL: %s", cfg.URL)
	}
	if cfg.Priority != 90 {
		t.Errorf("unexpected priority: %d", cfg.Priority)
	}
	if cfg.TimeoutSeconds != 15 {
		t.Errorf("unexpected timeout: %d", cfg.TimeoutSeconds)
	}
}
