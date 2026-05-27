package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/mcp/transport"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ServerClient manages transport to an external MCP server.
type ServerClient struct {
	name   string
	tport  transport.Transport
	nextID atomic.Int64
}

// NewServerClient creates a transport to an external MCP server and initializes it.
func NewServerClient(ctx context.Context, name string, cfg config.MCPServerConfig, bus *event.EventBus) (*ServerClient, error) {
	var tport transport.Transport
	var err error

	switch cfg.Type {
	case "stdio":
		tport, err = transport.NewStdio(name, cfg)
	case "sse":
		tport, err = transport.NewSSE(name, cfg)
	case "http-stream":
		tport, err = transport.NewHTTPStream(name, cfg, bus)
	default:
		return nil, fmt.Errorf("unsupported mcp server type: %q (supported: stdio, sse, http-stream)", cfg.Type)
	}
	if err != nil {
		return nil, err
	}

	sc := &ServerClient{
		name:  name,
		tport: tport,
	}

	timeout := config.TimeoutDuration(cfg.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := tport.Connect(ctx); err != nil {
		tport.Close()
		return nil, fmt.Errorf("connect mcp server %q: %w", name, err)
	}

	if err := sc.initialize(ctx); err != nil {
		tport.Close()
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
	if _, err := c.tport.SendRequest(ctx, initReq); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	if err := c.tport.SendNotification(ctx, notif); err != nil {
		return err
	}

	zap.S().Debugw("mcp server initialized", "server", c.name)
	return nil
}

// ListTools discovers tools from the server.
func (c *ServerClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	raw, err := c.tport.SendRequest(ctx, req)
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
	tr := otel.Tracer("dolphin/mcp")
	ctx, span := tr.Start(ctx, "mcp.server.call",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("server.name", c.name),
		attribute.String("tool.name", name),
		attribute.String("input", truncateString(string(arguments), 1024)),
	)
	defer span.End()

	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			zap.S().Warnw("failed to unmarshal tool arguments", "error", err)
		}
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
	raw, err := c.tport.SendRequest(ctx, req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		zap.S().Warnw("mcp server tool call failed", "server", c.name, "tool", name, "error", err)
		return &ToolResult{Content: fmt.Sprintf("MCP server %q unavailable: %v", c.name, err), IsError: true}, nil
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		zap.S().Warnw("mcp server returned unparseable result", "server", c.name, "tool", name, "error", err)
		return &ToolResult{Content: fmt.Sprintf("MCP server %q returned invalid data", c.name), IsError: true}, nil
	}

	var text string
	for _, block := range result.Content {
		if block.Type == "text" || block.Type == "" {
			text += block.Text
		}
	}

	if result.IsError {
		span.SetStatus(codes.Error, truncateString(text, 256))
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.SetAttributes(attribute.String("output", truncateString(text, 2048)))

	return &ToolResult{Content: text, IsError: result.IsError}, nil
}

// Close shuts down the transport to the MCP server.
func (c *ServerClient) Close() error {
	zap.S().Debugw("shutting down mcp server", "server", c.name)
	return c.tport.Close()
}
