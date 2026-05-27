// Package webhost provides MCP tools for native browser control via the WebHost
// native application (Swift on macOS, WPF+WebView2 on Windows). It connects to
// the WebHost HTTP server and forwards tool calls as JSON-RPC 2.0 requests.
package webhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tool implements native browser control via WebHost.
type Tool struct {
	cfg    *config.MCPWebHostConfig
	schema json.RawMessage
	client *http.Client
	nextID int64
}

// webhostInput is the combined input for all webhost actions.
type webhostInput struct {
	Action      string `json:"action"`
	SessionID   string `json:"sessionId"`
	URL         string `json:"url"`
	Script      string `json:"script"`
	Timeout     int    `json:"timeout"`
	CSS         string `json:"css"`
	JS          string `json:"js"`
	Selector    string `json:"selector"`
	Interactive bool   `json:"interactive"`
	DialogID    string `json:"dialogId"`
	DialogText  string `json:"dialogText"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	TabID       string `json:"tabId"`
}

// jsonRPCRequest is a JSON-RPC 2.0 request for the WebHost endpoint.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCCallParams is the params envelope for tools/call.
type jsonRPCCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// jsonRPCResponse is a minimal JSON-RPC 2.0 response decoder.
type jsonRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(cfg *config.Config) *Tool {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: web_session_create, page_open, script_run, page_screenshot, web_inject, web_wait, web_set_interactive, web_capabilities, web_session_close, web_dialog_response, tab_list, tab_switch, tab_create, tab_close, go_back, go_forward",
				"enum":        []string{"web_session_create", "page_open", "script_run", "page_screenshot", "web_inject", "web_wait", "web_set_interactive", "web_capabilities", "web_session_close", "web_dialog_response", "tab_list", "tab_switch", "tab_create", "tab_close", "go_back", "go_forward"},
			},
			"sessionId": map[string]any{
				"type":        "string",
				"description": "Session ID returned from web_session_create. Required for all actions except web_session_create and web_capabilities.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "URL for page_open action",
			},
			"script": map[string]any{
				"type":        "string",
				"description": "JavaScript code for script_run action. Supports async/await.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in milliseconds for script_run (default 10000) or web_wait (default 30000)",
			},
			"css": map[string]any{
				"type":        "string",
				"description": "CSS to inject for web_inject action",
			},
			"js": map[string]any{
				"type":        "string",
				"description": "JavaScript to inject for web_inject action",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector for web_wait action",
			},
			"interactive": map[string]any{
				"type":        "boolean",
				"description": "Enable/disable interactive mode for web_set_interactive action",
			},
			"dialogId": map[string]any{
				"type":        "string",
				"description": "Dialog ID from a web/dialog event, for web_dialog_response action",
			},
			"dialogText": map[string]any{
				"type":        "string",
				"description": "Text to enter for prompt dialogs, for web_dialog_response action",
			},
			"width": map[string]any{
				"type":        "integer",
				"description": "Viewport width for web_session_create action",
			},
			"height": map[string]any{
				"type":        "integer",
				"description": "Viewport height for web_session_create action",
			},
			"tabId": map[string]any{
				"type":        "string",
				"description": "Tab ID for tab management actions (tab_switch, tab_close) and navigation (go_back, go_forward)",
			},
		},
		"required": []string{"action"},
	})

	timeout := webhostTimeout(cfg.MCP.Webhost)
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	return &Tool{
		cfg:    &cfg.MCP.Webhost,
		schema: schema,
		client: &http.Client{Timeout: timeout, Transport: tr},
	}
}

func (w *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "webhost",
		Description: "Control a native browser via the WebHost native UI application. Supports creating browser sessions, navigating to URLs, executing JavaScript (the universal operation for clicks, form fills, and scraping), taking screenshots, injecting CSS/JS, waiting for elements, switching between observation and interactive modes, and responding to JavaScript dialogs. The browser state persists across calls within the same session. Requires WebHost app running (default http://localhost:9223).",
		InputSchema: w.schema,
		Priority:    w.cfg.Priority,
		Source:      "built-in",
	}
}

func (w *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params webhostInput
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if params.Action == "" {
		return &mcp.ToolResult{Content: "missing required field: action", IsError: true}, nil
	}

	// Build arguments for the JSON-RPC call.
	args := make(map[string]any)
	if params.SessionID != "" {
		args["sessionId"] = params.SessionID
	}
	if params.TabID != "" {
		args["tabId"] = params.TabID
	}
	switch params.Action {
	case "web_session_create":
		if params.Width > 0 || params.Height > 0 {
			vp := make(map[string]int)
			if params.Width > 0 {
				vp["width"] = params.Width
			}
			if params.Height > 0 {
				vp["height"] = params.Height
			}
			args["viewport"] = vp
		}
	case "page_open":
		args["url"] = params.URL
	case "script_run":
		args["script"] = params.Script
		if params.Timeout > 0 {
			args["timeout"] = params.Timeout
		}
	case "page_screenshot":
		// sessionId only
	case "web_inject":
		if params.CSS != "" {
			args["css"] = params.CSS
		}
		if params.JS != "" {
			args["js"] = params.JS
		}
	case "web_wait":
		args["selector"] = params.Selector
		if params.Timeout > 0 {
			args["timeout"] = params.Timeout
		}
	case "web_set_interactive":
		args["interactive"] = params.Interactive
	case "web_capabilities":
		// sessionId is optional
	case "web_session_close":
		// sessionId only
	case "web_dialog_response":
		args["dialogId"] = params.DialogID
		if params.DialogText != "" {
			args["text"] = params.DialogText
		}
		// Default action is dismiss if not specified via interactive flag:
		// interactive=true → accept, interactive=false → dismiss
		if params.Interactive {
			args["action"] = "accept"
		} else {
			args["action"] = "dismiss"
		}
	case "tab_list":
		// sessionId only
	case "tab_switch":
		// sessionId + tabId required
	case "tab_create":
		if params.URL != "" {
			args["url"] = params.URL
		}
	case "tab_close":
		// sessionId + tabId required
	case "go_back":
		// sessionId only; tabId optional
	case "go_forward":
		// sessionId only; tabId optional
	default:
		return &mcp.ToolResult{Content: fmt.Sprintf("unknown action: %q", params.Action), IsError: true}, nil
	}

	return w.callTool(ctx, params.Action, args)
}

// callTool sends a JSON-RPC tools/call request to the WebHost server.
func (w *Tool) callTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	tr := otel.Tracer("dolphin/mcp")
	ctx, span := tr.Start(ctx, "webhost.call",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("webhost.action", toolName),
		attribute.String("webhost.url", w.cfg.URL),
	)
	defer span.End()

	w.nextID++
	body := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      w.nextID,
		Method:  "tools/call",
		Params: jsonRPCCallParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return &mcp.ToolResult{Content: fmt.Sprintf("encode request: %v", err), IsError: true}, nil
	}
	span.SetAttributes(attribute.String("input", truncateString(string(payload), 1024)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return &mcp.ToolResult{Content: fmt.Sprintf("create request: %v", err), IsError: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return &mcp.ToolResult{
			Content: fmt.Sprintf("WebHost connection failed (%s): %v\n\nMake sure WebHost app is running (default: http://localhost:9223)", w.cfg.URL, err),
			IsError: true,
		}, nil
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return &mcp.ToolResult{Content: fmt.Sprintf("read response: %v", err), IsError: true}, nil
	}

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return &mcp.ToolResult{
			Content: fmt.Sprintf("WebHost returned HTTP %d: %s", resp.StatusCode, string(respBody)),
			IsError: true,
		}, nil
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return &mcp.ToolResult{Content: fmt.Sprintf("parse response: %v", err), IsError: true}, nil
	}

	if rpcResp.Error != nil {
		span.SetAttributes(
			attribute.Int("webhost.error_code", rpcResp.Error.Code),
		)
		span.SetStatus(codes.Error, rpcResp.Error.Message)
		return &mcp.ToolResult{
			Content: fmt.Sprintf("WebHost error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message),
			IsError: true,
		}, nil
	}

	// Pretty-print the result JSON for the LLM.
	var result any
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		output := string(rpcResp.Result)
		span.SetAttributes(attribute.String("output", truncateString(output, 2048)))
		span.SetStatus(codes.Ok, "")
		return &mcp.ToolResult{Content: output}, nil
	}
	formatted, _ := json.MarshalIndent(result, "", "  ")
	output := string(formatted)
	span.SetAttributes(attribute.String("output", truncateString(output, 2048)))
	span.SetStatus(codes.Ok, "")
	return &mcp.ToolResult{Content: output}, nil
}

func webhostTimeout(cfg config.MCPWebHostConfig) time.Duration {
	if cfg.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(cfg.TimeoutSeconds) * time.Second
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
