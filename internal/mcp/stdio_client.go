package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"dolphin/internal/types"
)

// StdioClient is an MCP client that communicates with a subprocess via stdin/stdout.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	sc     *bufio.Scanner
	mu     sync.Mutex
	nextID int
}

func NewStdioClient(ctx context.Context, command string, args []string) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("mcp stdio: start: %w", err)
	}

	return &StdioClient{
		cmd:   cmd,
		stdin: stdin,
		sc:    bufio.NewScanner(stdout),
	}, nil
}

func (c *StdioClient) List(ctx context.Context) ([]types.ToolDef, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp stdio: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var result toolListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
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

func (c *StdioClient) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	var args any
	if call.Arguments != "" {
		json.Unmarshal([]byte(call.Arguments), &args)
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
			Content:    fmt.Sprintf("mcp stdio error: %s", resp.Error.Message),
			IsError:    true,
		}, nil
	}

	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    string(resp.Result),
	}, nil
}

func (c *StdioClient) Close() error {
	c.stdin.Close()
	return c.cmd.Process.Kill()
}

func (c *StdioClient) call(ctx context.Context, method string, params any) (*jsonRPCResponse, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: marshal: %w", err)
	}

	c.mu.Lock()
	_, err = fmt.Fprintln(c.stdin, string(data))
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: write: %w", err)
	}

	for c.sc.Scan() {
		line := c.sc.Text()
		if line == "" {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		return &resp, nil
	}

	if err := c.sc.Err(); err != nil {
		return nil, fmt.Errorf("mcp stdio: read: %w", err)
	}
	return nil, fmt.Errorf("mcp stdio: connection closed")
}
