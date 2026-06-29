package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"dolphin/internal/types"
)

// ClientOption configures an MCP Client.
type ClientOption func(*Client)

// WithHeaders sets custom HTTP headers sent with every request.
func WithHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		c.headers = headers
	}
}

// Client is an MCP Streamable HTTP client implementing JSON-RPC over HTTP
// with support for SSE streaming responses (MCP Streamable HTTP transport).
// Reference: https://spec.modelcontextprotocol.io/specification/2025-03-26/#streamable-http
type Client struct {
	baseURL    string
	sessionID  string
	http       *http.Client
	mu         sync.Mutex
	nextID     int
	onProgress func(event string, data json.RawMessage)
	headers    map[string]string
}

func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		http:    &http.Client{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetOnProgress sets a callback that receives intermediate SSE events
// (e.g. "progress" notifications) during long-running tool calls.
// The callback is called synchronously from the readSSE loop.
func (c *Client) SetOnProgress(fn func(event string, data json.RawMessage)) {
	c.onProgress = fn
}

// SetHeaders sets custom HTTP headers sent with every request.
func (c *Client) SetHeaders(headers map[string]string) {
	c.headers = headers
}

func (c *Client) List(ctx context.Context) ([]types.ToolDef, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("mcp: unmarshal tools: %w", err)
	}

	defs := make([]types.ToolDef, 0, len(result.Tools))
	for _, t := range result.Tools {
		defs = append(defs, types.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Schema:      t.InputSchema,
		})
	}
	return defs, nil
}

func (c *Client) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args any
	if call.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return nil, fmt.Errorf("mcp: parse arguments: %w", err)
		}
	}

	resp, err := c.call(ctx, "tools/call", map[string]any{
		"name":      call.Name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return &types.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("mcp error: %s", resp.Error.Message),
			IsError:    true,
		}, nil
	}

	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    string(resp.Result),
	}, nil
}

// call sends a JSON-RPC request via the Streamable HTTP transport.
// It handles both application/json and text/event-stream responses.
func (c *Client) call(ctx context.Context, method string, params any) (*jsonRPCResponse, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mcp: request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: http: %w", err)
	}
	defer resp.Body.Close()

	// Track session ID from response.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	contentType := resp.Header.Get("Content-Type")

	// SSE streaming response.
	if strings.HasPrefix(contentType, "text/event-stream") {
		return c.readSSE(resp.Body)
	}

	// Standard JSON response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: http status %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("mcp: unmarshal: %w", err)
	}
	return &rpcResp, nil
}

// LazyClient defers connection to the first List/Execute call, allowing MCP
// server sources to be registered at boot time even if the server isn't
// running yet. Each call creates the real Client on demand.
type LazyClient struct {
	url     string
	headers map[string]string
	client  *Client
	mu      sync.Mutex
}

func NewLazyClient(url string) *LazyClient {
	return &LazyClient{url: url}
}

// SetHeaders sets custom HTTP headers for the lazy client.
// Must be called before the first List/Execute call.
func (l *LazyClient) SetHeaders(headers map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.headers = headers
	if l.client != nil {
		l.client.headers = headers
	}
}

func (l *LazyClient) List(ctx context.Context) ([]types.ToolDef, error) {
	return l.getClient().List(ctx)
}

func (l *LazyClient) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	return l.getClient().Execute(ctx, call)
}

func (l *LazyClient) getClient() *Client {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client == nil {
		l.client = NewClient(l.url)
		l.client.headers = l.headers
	}
	return l.client
}

// readSSE parses a text/event-stream response and extracts the final
// JSON-RPC response from the stream. Intermediate events (e.g. progress
// notifications) are forwarded to the Client's onProgress callback.
func (c *Client) readSSE(body io.Reader) (*jsonRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	// SSE streams can leave long-lived connections open; increase buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event, data string

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			// Empty line delimits an event.
			if data == "" {
				continue
			}
			// Return on result or error event.
			if event == "result" || event == "error" || event == "" {
				var rpcResp jsonRPCResponse
				if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
					event = ""
					data = ""
					continue
				}
				return &rpcResp, nil
			}
			// Forward intermediate events (e.g. progress notifications).
			if c.onProgress != nil && data != "" {
				c.onProgress(event, json.RawMessage(data))
			}
			event = ""
			data = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp: sse read: %w", err)
	}

	// Handle trailing data without trailing newline.
	if data != "" {
		var rpcResp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
			return &rpcResp, nil
		}
	}

	return nil, fmt.Errorf("mcp: no result in SSE stream")
}
