package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"dolphinzZ/internal/config"
)

// workdirKey is used to pass a working directory through context for workspace isolation.
type workdirKey struct{}

// WithWorkdir returns a context that sets the working directory for shell tool execution.
func WithWorkdir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workdirKey{}, dir)
}

// ShellTool implements shell command execution via MCP.
type ShellTool struct {
	cfg    *config.ShellConfig
	schema json.RawMessage
}

func NewShellTool(cfg *config.Config) *ShellTool {
	if len(cfg.MCP.Shell.AllowedCommands) == 0 {
		slog.Warn("shell tool: no command restrictions — all commands are allowed. " +
			"Set mcp.shell.allowed_commands in config to restrict.")
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (optional)",
			},
		},
		"required": []string{"command"},
	})

	return &ShellTool{
		cfg:    &cfg.MCP.Shell,
		schema: schema,
	}
}

func (s *ShellTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "shell",
		Description: "Execute a shell command and return its output. Use this for file operations, running scripts, and interacting with the system.",
		InputSchema: s.schema,
	}
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	// Check command whitelist (if configured)
	if err := s.checkAllowed(params.Command); err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	timeout := s.cfg.TimeoutSeconds
	if params.Timeout > 0 {
		timeout = params.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	slog.Debug("executing shell command", "command", params.Command)

	cmd := exec.CommandContext(ctx, "sh", "-c", params.Command)
	// Use workspace directory from context if set (for sub-agent workspace isolation)
	if wd, ok := ctx.Value(workdirKey{}).(string); ok && wd != "" {
		cmd.Dir = wd
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return &ToolResult{
				Content: fmt.Sprintf("command timed out after %ds:\nstdout:\n%s\nstderr:\n%s", timeout, stdout.String(), stderr.String()),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("command failed:\nstdout:\n%s\nstderr:\n%s\nerror: %v", stdout.String(), stderr.String(), err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String()),
	}, nil
}

func (s *ShellTool) checkAllowed(fullCommand string) error {
	allowed := s.cfg.AllowedCommands
	if len(allowed) == 0 {
		return nil // empty = allow all
	}
	cmdName := strings.Fields(fullCommand)[0]
	for _, a := range allowed {
		if strings.HasPrefix(fullCommand, a) {
			return nil
		}
	}
	// Also check just the command name
	for _, a := range allowed {
		if cmdName == a {
			return nil
		}
	}
	return fmt.Errorf("command not allowed: %s (allowed: %v)", cmdName, allowed)
}
