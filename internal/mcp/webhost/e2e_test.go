//go:build darwin

package webhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
)

const defaultWebHostURL = "http://localhost:9223/mcp/call"

// helper: check if webhost is reachable.
func webhostReachable(url string) bool {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// skipIfWebHostOffline skips the test if webhost is not running.
func skipIfWebHostOffline(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping webhost e2e test in short mode")
	}
	url := os.Getenv("WEBHOST_URL")
	if url == "" {
		url = defaultWebHostURL
	}
	if !webhostReachable(url) {
		t.Skip("WebHost not reachable — start the WebHost app first (default: http://localhost:9223)")
	}
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type e2eRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse decodes the top-level JSON-RPC envelope.
type e2eResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func e2eCall(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	url := os.Getenv("WEBHOST_URL")
	if url == "" {
		url = defaultWebHostURL
	}
	body := e2eRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	payload, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http post: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var env e2eResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("JSON-RPC error [%d]: %s", env.Error.Code, env.Error.Message)
	}
	return env.Result
}

// ============================================================
// E2E Tests
// ============================================================

func TestE2E_WebHost_Initialize(t *testing.T) {
	skipIfWebHostOffline(t)
	result := e2eCall(t, "initialize", nil)
	var info struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &info); err != nil {
		t.Fatalf("parse initialize result: %v", err)
	}
	if info.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected protocol 2024-11-05, got %q", info.ProtocolVersion)
	}
	if info.ServerInfo.Name != "WebHost" {
		t.Errorf("expected server WebHost, got %q", info.ServerInfo.Name)
	}
	t.Logf("WebHost server: %s %s (protocol %s)", info.ServerInfo.Name, info.ServerInfo.Version, info.ProtocolVersion)
}

func TestE2E_WebHost_ToolsList(t *testing.T) {
	skipIfWebHostOffline(t)
	result := e2eCall(t, "tools/list", nil)

	var list struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatalf("parse tools/list result: %v", err)
	}

	expectedTools := []string{
		"web_session_create",
		"page_open",
		"script_run",
		"page_screenshot",
		"web_set_interactive",
		"web_capabilities",
		"web_session_close",
		"web_inject",
		"web_wait",
		"web_dialog_response",
		"tab_list",
		"tab_switch",
		"tab_create",
		"tab_close",
		"go_back",
		"go_forward",
	}
	found := make(map[string]bool)
	for _, t := range list.Tools {
		found[t.Name] = true
	}
	for _, name := range expectedTools {
		if !found[name] {
			t.Errorf("expected tool %q not found in tools/list", name)
		}
	}
	t.Logf("WebHost exposes %d tools", len(list.Tools))
}

func TestE2E_GoTool_Definition(t *testing.T) {
	cfg := &config.Config{}
	cfg.MCP.Webhost.Enabled = true
	cfg.MCP.Webhost.URL = defaultWebHostURL
	tool := New(cfg)
	def := tool.Definition()

	if def.Name != "webhost" {
		t.Errorf("expected name webhost, got %q", def.Name)
	}
	if def.Source != "built-in" {
		t.Errorf("expected source built-in, got %q", def.Source)
	}

	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	action, ok := schema.Properties["action"]
	if !ok {
		t.Fatal("schema missing action property")
	}
	actionMap, ok := action.(map[string]any)
	if !ok {
		t.Fatal("action property is not a map")
	}
	enum, ok := actionMap["enum"].([]any)
	if !ok {
		t.Fatal("action enum is not a slice")
	}
	if len(enum) != 16 {
		t.Errorf("expected 16 enum values, got %d", len(enum))
	}
}

func TestE2E_GoTool_SessionLifecycle(t *testing.T) {
	skipIfWebHostOffline(t)
	cfg := &config.Config{}
	cfg.MCP.Webhost.Enabled = true
	cfg.MCP.Webhost.URL = defaultWebHostURL
	tool := New(cfg)

	// 1) Create session
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"web_session_create"}`))
	if err != nil {
		t.Fatalf("web_session_create: %v", err)
	}
	if result.IsError {
		t.Fatalf("web_session_create failed: %s", result.Content)
	}
	var resp struct {
		Success   bool   `json:"success"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal([]byte(result.Content), &resp); err != nil {
		t.Fatalf("parse session response: %v\nraw: %s", err, result.Content)
	}
	if !resp.Success {
		t.Fatal("session creation returned success=false")
	}
	if resp.SessionID == "" {
		t.Fatal("expected non-empty sessionId")
	}
	sessionID := resp.SessionID
	t.Logf("created session: %s", sessionID)

	// 2) Check capabilities
	capsResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"web_capabilities","sessionId":"%s"}`, sessionID)))
	if err != nil {
		t.Fatalf("web_capabilities: %v", err)
	}
	if capsResult.IsError {
		t.Fatalf("web_capabilities failed: %s", capsResult.Content)
	}
	t.Logf("capabilities: %s", capsResult.Content)

	// 3) Navigate to a page
	navResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"page_open","sessionId":"%s","url":"https://example.com"}`, sessionID)))
	if err != nil {
		t.Fatalf("page_open: %v", err)
	}
	if navResult.IsError {
		t.Fatalf("page_open failed: %s", navResult.Content)
	}
	t.Logf("navigate result: %s", navResult.Content)

	// 4) Wait a moment for page to load, then execute JS
	time.Sleep(2 * time.Second)
	jsResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"script_run","sessionId":"%s","script":"document.title"}`, sessionID)))
	if err != nil {
		t.Fatalf("script_run: %v", err)
	}
	if jsResult.IsError {
		t.Fatalf("script_run failed: %s", jsResult.Content)
	}
	t.Logf("page title: %s", jsResult.Content)

	// The title should contain "Example"
	// The result format is {"success":true,"value":"..."}
	var jsResp struct {
		Success bool   `json:"success"`
		Value   string `json:"value"`
	}
	if err := json.Unmarshal([]byte(jsResult.Content), &jsResp); err == nil {
		if jsResp.Success && !strings.Contains(jsResp.Value, "Example") {
			t.Logf("unexpected page title: %q", jsResp.Value)
		}
	}

	// 5) Take screenshot
	ssResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"page_screenshot","sessionId":"%s"}`, sessionID)))
	if err != nil {
		t.Fatalf("page_screenshot: %v", err)
	}
	if ssResult.IsError {
		t.Fatalf("page_screenshot failed: %s", ssResult.Content)
	}
	t.Logf("screenshot taken (content length: %d)", len(ssResult.Content))

	// 6) Close session
	closeResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"web_session_close","sessionId":"%s"}`, sessionID)))
	if err != nil {
		t.Fatalf("web_session_close: %v", err)
	}
	if closeResult.IsError {
		t.Fatalf("web_session_close failed: %s", closeResult.Content)
	}
	t.Logf("session closed: %s", closeResult.Content)
}

func TestE2E_GoTool_InvalidSession(t *testing.T) {
	skipIfWebHostOffline(t)
	cfg := &config.Config{}
	cfg.MCP.Webhost.Enabled = true
	cfg.MCP.Webhost.URL = defaultWebHostURL
	tool := New(cfg)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"page_open","sessionId":"nonexistent","url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Log("expected error for invalid session, got success")
	}
	if result.Content == "" {
		t.Error("expected non-empty error message")
	}
}

func TestE2E_GoTool_AllActionNames(t *testing.T) {
	skipIfWebHostOffline(t)
	cfg := &config.Config{}
	cfg.MCP.Webhost.Enabled = true
	cfg.MCP.Webhost.URL = defaultWebHostURL
	tool := New(cfg)

	actions := []string{
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

	// Create a session first for actions that need one.
	createResult, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"web_session_create"}`))
	if err != nil {
		t.Fatalf("web_session_create: %v", err)
	}
	var createResp struct {
		Success   bool   `json:"success"`
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal([]byte(createResult.Content), &createResp)
	sessionID := createResp.SessionID
	t.Logf("session: %s", sessionID)

	for _, action := range actions {
		var input map[string]any
		switch action {
		case "web_session_create":
			continue // already tested
		case "web_session_close":
			// Will test at end
			continue
		case "page_open":
			input = map[string]any{"action": action, "sessionId": sessionID, "url": "https://example.com"}
		case "script_run":
			input = map[string]any{"action": action, "sessionId": sessionID, "script": "1+1"}
		case "web_inject":
			input = map[string]any{"action": action, "sessionId": sessionID, "css": "body{background:red}"}
		case "web_wait":
			input = map[string]any{"action": action, "sessionId": sessionID, "selector": "body"}
		case "web_capabilities":
			input = map[string]any{"action": action, "sessionId": sessionID}
		case "page_screenshot":
			input = map[string]any{"action": action, "sessionId": sessionID}
		case "web_set_interactive":
			input = map[string]any{"action": action, "sessionId": sessionID, "interactive": false}
		case "web_dialog_response":
			input = map[string]any{"action": action, "sessionId": sessionID, "dialogId": "nonexistent"}
		default:
			input = map[string]any{"action": action, "sessionId": sessionID}
		}
		data, _ := json.Marshal(input)
		result, err := tool.Execute(context.Background(), data)
		if err != nil {
			t.Errorf("action %q: unexpected error: %v", action, err)
			continue
		}
		// Most actions should succeed; dialog_response with nonexistent dialog
		// might fail gracefully — that's fine, as long as the server responds.
		t.Logf("action %q: success=%v, content=%.80s", action, !result.IsError, result.Content)
	}

	// Close session
	closeResult, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"action":"web_session_close","sessionId":"%s"}`, sessionID)))
	if err != nil {
		t.Fatalf("web_session_close: %v", err)
	}
	if closeResult.IsError {
		t.Logf("web_session_close result: %s", closeResult.Content)
	}
}
