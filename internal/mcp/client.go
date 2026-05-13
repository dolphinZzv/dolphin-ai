package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// ServerClient manages transport to an external MCP server.
type ServerClient struct {
	name      string
	transport mcpTransport
	nextID    atomic.Int64
}

// NewServerClient creates a transport to an external MCP server and initializes it.
func NewServerClient(name string, cfg config.MCPServerConfig) (*ServerClient, error) {
	var transport mcpTransport
	var err error

	switch cfg.Type {
	case "stdio":
		transport, err = newStdioTransport(name, cfg)
	case "sse":
		transport, err = newSSETransport(name, cfg)
	case "http-stream":
		transport, err = newHTTPStreamTransport(name, cfg)
	default:
		return nil, fmt.Errorf("unsupported mcp server type: %q (supported: stdio, sse, http-stream)", cfg.Type)
	}
	if err != nil {
		return nil, err
	}

	sc := &ServerClient{
		name:      name,
		transport: transport,
	}

	timeout := config.TimeoutDuration(cfg.Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := transport.connect(ctx); err != nil {
		transport.close()
		return nil, fmt.Errorf("connect mcp server %q: %w", name, err)
	}

	if err := sc.initialize(ctx); err != nil {
		transport.close()
		return nil, fmt.Errorf("initialize mcp server %q: %w", name, err)
	}

	return sc, nil
}

// initialize performs MCP handshake.
func (c *ServerClient) initialize(ctx context.Context) error {
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
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
	if _, err := c.transport.sendRequest(ctx, initReq); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	if err := c.transport.sendNotification(ctx, notif); err != nil {
		return err
	}

	zap.S().Debugw("mcp server initialized", "server", c.name)
	return nil
}

// ListTools discovers tools from the server.
func (c *ServerClient) ListTools() ([]ToolDefinition, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	raw, err := c.transport.sendRequest(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	defs := make([]ToolDefinition, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		defs = append(defs, ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return defs, nil
}

// CallTool executes a tool on the server.
func (c *ServerClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolResult, error) {
	var args map[string]any
	if len(arguments) > 0 {
		json.Unmarshal(arguments, &args)
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := c.transport.sendRequest(ctx, req)
	if err != nil {
		zap.S().Warnw("mcp server tool call failed", "server", c.name, "tool", name, "error", err)
		return &ToolResult{Content: fmt.Sprintf("MCP server %q is unavailable", c.name), IsError: true}, nil
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		zap.S().Warnw("mcp server returned unparseable result", "server", c.name, "tool", name, "error", err)
		return &ToolResult{Content: fmt.Sprintf("MCP server %q returned invalid data", c.name), IsError: true}, nil
	}

	var text string
	for _, block := range result.Content {
		if block.Type == "text" || block.Type == "" {
			text += block.Text
		}
	}
	return &ToolResult{Content: text, IsError: result.IsError}, nil
}

// Close shuts down the transport to the MCP server.
func (c *ServerClient) Close() error {
	zap.S().Debugw("shutting down mcp server", "server", c.name)
	return c.transport.close()
}
