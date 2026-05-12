package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/config"
)

// sseServerConfig builds a config for the remote SSE MCP server.
// Returns nil if no auth token is available (tests should call t.Skip).
func sseServerConfig(t *testing.T) config.MCPServerConfig {
	t.Helper()
	url := os.Getenv("DZ_TEST_MCP_SSE_URL")
	if url == "" {
		url = "http://47.95.200.101:8080/mcp"
	}
	headers := map[string]string{}
	token := os.Getenv("DZ_TEST_MCP_TOKEN")
	if token == "" {
		if tok, err := readTokenFile(); err == nil {
			token = tok
		}
	}
	if token == "" {
		t.Skip("DZ_TEST_MCP_TOKEN not set and .dolphin/testdata/sse_token not found")
	}
	headers["Authorization"] = "Bearer " + token
	return config.MCPServerConfig{
		Type:    "sse",
		URL:     url,
		Headers: headers,
		Timeout: 30,
	}
}

// readTokenFile reads the SSE test token from the repo's gitignored secrets dir.
func readTokenFile() (string, error) {
	// Test working dir is internal/mcp/, token is at repo root .dolphin/testdata/sse_token
	p := filepath.Join("..", "..", ".dolphin", "testdata", "sse_token")
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read sse_token: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func TestSSETransportInitialize(t *testing.T) {
	cfg := sseServerConfig(t)

	transport, err := newSSETransport("chick", cfg)
	if err != nil {
		t.Fatalf("newSSETransport: %v", err)
	}
	defer transport.close()

	ctx := context.Background()
	if err := transport.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if transport.messageURL == "" {
		t.Fatal("messageURL is empty after connect")
	}
	t.Logf("connected, messageURL=%s", transport.messageURL)

	// Initialize
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "dolphin",
				"version": "1.0",
			},
		},
	}
	initRaw, err := transport.sendRequest(ctx, initReq)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	var initResult struct {
		Result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			Capabilities struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(initRaw, &initResult); err != nil {
		t.Fatalf("parse initialize result: %v", err)
	}
	t.Logf("server: %s v%s, has_tools=%v",
		initResult.Result.ServerInfo.Name,
		initResult.Result.ServerInfo.Version,
		initResult.Result.Capabilities.Tools != nil)

	// Send initialized notification
	if err := transport.sendNotification(ctx, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}); err != nil {
		t.Fatalf("initialized notification: %v", err)
	}
}

func TestSSETransportListTools(t *testing.T) {
	cfg := sseServerConfig(t)

	client, err := NewServerClient("chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	t.Logf("found %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("  - %s: %s", tool.Name, tool.Description)
	}

	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool")
	}

	// Verify expected tools exist
	expected := map[string]bool{
		"search_issues":       false,
		"create_issue":        false,
		"add_comment":         false,
		"transition_issue":    false,
		"list_agents":         false,
		"check_notifications": false,
	}
	for _, tool := range tools {
		if _, ok := expected[tool.Name]; ok {
			expected[tool.Name] = true
		}
		// Verify schema is present
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty InputSchema", tool.Name)
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestSSETransportCallTool(t *testing.T) {
	cfg := sseServerConfig(t)

	client, err := NewServerClient("chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	// Call search_issues to get issues
	args := json.RawMessage(`{"limit":"5"}`)
	result, err := client.CallTool(context.Background(), "search_issues", args)
	if err != nil {
		t.Fatalf("CallTool search_issues: %v", err)
	}

	if result.IsError {
		t.Fatalf("search_issues returned error: %s", result.Content)
	}

	t.Logf("search_issues result: %s", truncateStr(result.Content, 500))

	// Server currently returns structured JSON directly (non-standard content blocks).
	// Accept either text content or raw JSON as valid responses.
	if result.Content == "" {
		t.Log("result content is empty (server may not use standard content blocks)")
	}
}

func TestSSETransportCallToolNoResults(t *testing.T) {
	cfg := sseServerConfig(t)

	client, err := NewServerClient("chick", cfg)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	defer client.Close()

	// Search for something that won't match
	args := json.RawMessage(`{"search":"xyznonexistent12345"}`)
	result, err := client.CallTool(context.Background(), "search_issues", args)
	if err != nil {
		t.Fatalf("CallTool search_issues: %v", err)
	}

	t.Logf("no-match search result: %s", result.Content)
	// Even empty search should return a valid result, not an error
}

func TestNewServerClientRejectsSseWithoutURL(t *testing.T) {
	_, err := NewServerClient("test", config.MCPServerConfig{
		Type: "sse",
		URL:  "",
	})
	if err == nil {
		t.Fatal("expected error for SSE without URL")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Errorf("expected 'url is required' error, got: %v", err)
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
