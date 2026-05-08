package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"dolphinzZ/internal/config"
)

// ServerClient manages an external MCP server subprocess.
type ServerClient struct {
	cfg    config.MCPServerConfig
	name   string
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID atomic.Int64
}

// NewServerClient starts an external MCP server and initializes it.
func NewServerClient(name string, cfg config.MCPServerConfig) (*ServerClient, error) {
	if cfg.Type != "stdio" {
		return nil, fmt.Errorf("unsupported mcp server type: %s", cfg.Type)
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp server %q: command is required", name)
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Stderr is inherited (goes to the parent's stderr)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp server %q: %w", name, err)
	}

	sc := &ServerClient{
		cfg:    cfg,
		name:   name,
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdin),
		stdout: bufio.NewScanner(bufio.NewReader(stdout)),
	}

	// Initialize
	if err := sc.initialize(); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("initialize mcp server %q: %w", name, err)
	}

	return sc, nil
}

// initialize performs MCP handshake.
func (c *ServerClient) initialize() error {
	// Send initialize request
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "dolphinzZ",
				"version": "1.0",
			},
		},
	}
	if _, err := c.sendRequest(initReq); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	if err := c.writeJSON(notif); err != nil {
		return err
	}

	slog.Debug("mcp server initialized", "server", c.name)
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
	raw, err := c.sendRequest(req)
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
		// Default empty schema if missing
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
	raw, err := c.sendRequest(req)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return &ToolResult{Content: fmt.Sprintf("parse result: %v", err), IsError: true}, nil
	}

	// Concatenate text content blocks
	var text string
	for _, block := range result.Content {
		if block.Type == "text" || block.Type == "" {
			text += block.Text
		}
	}
	return &ToolResult{Content: text, IsError: result.IsError}, nil
}

// Close gracefully shuts down the server process: SIGTERM, wait 3s, then SIGKILL.
func (c *ServerClient) Close() error {
	slog.Debug("shutting down mcp server", "server", c.name)

	// Try graceful shutdown with SIGTERM first
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Interrupt not supported (e.g. Windows), fall back to Kill
		slog.Debug("interrupt not supported, killing mcp server", "server", c.name)
		return c.cmd.Process.Kill()
	}

	// Wait up to 3 seconds for graceful exit
	done := make(chan struct{})
	go func() {
		c.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Debug("mcp server exited gracefully", "server", c.name)
		return nil
	case <-time.After(3 * time.Second):
		slog.Warn("mcp server did not exit in time, killing", "server", c.name)
		return c.cmd.Process.Kill()
	}
}

// sendRequest sends a JSON-RPC request and waits for the response.
func (c *ServerClient) sendRequest(req map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.writeJSON(req); err != nil {
		return nil, err
	}

	for c.stdout.Scan() {
		line := c.stdout.Text()
		if line == "" {
			continue
		}

		var msg struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Skip responses that don't match our request ID
		reqID, _ := req["id"].(int64)
		if msg.ID != reqID {
			continue
		}

		if msg.Error != nil {
			return nil, fmt.Errorf("jsonrpc error: %s (code %d)", msg.Error.Message, msg.Error.Code)
		}

		return msg.Result, nil
	}

	if err := c.stdout.Err(); err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}
	return nil, fmt.Errorf("server closed connection")
}

func (c *ServerClient) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(data); err != nil {
		return err
	}
	if _, err := c.stdin.Write([]byte("\n")); err != nil {
		return err
	}
	return c.stdin.Flush()
}
