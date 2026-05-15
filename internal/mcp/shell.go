package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"

	"go.uber.org/zap"
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
	if len(cfg.MCP.Shell.AllowedCommands) == 0 && !cfg.MCP.Shell.AllowUnrestricted {
		zap.S().Warnw("shell tool: no command whitelist set — full shell access enabled. " +
			"Set mcp.shell.allowed_commands to restrict to specific commands, " +
			"or set mcp.shell.allow_unrestricted=false explicitly.")
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
		Priority:    s.cfg.Priority,
		Source:      "built-in",
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

	// Enforce command length limit
	maxLen := s.cfg.MaxCommandLength
	if maxLen <= 0 {
		maxLen = 4096
	}
	if len(params.Command) > maxLen {
		return &ToolResult{Content: fmt.Sprintf("command too long (%d bytes, max %d)", len(params.Command), maxLen), IsError: true}, nil
	}

	allowed := s.cfg.AllowedCommands
	fields := strings.Fields(params.Command)
	if len(fields) == 0 {
		return &ToolResult{Content: "empty command", IsError: true}, nil
	}
	if len(allowed) > 0 {
		// Restricted mode: strict allowlist, no shell
		if !s.isAllowed(fields[0]) {
			return &ToolResult{Content: fmt.Sprintf("command not allowed: %s (allowed: %v)", fields[0], allowed), IsError: true}, nil
		}
		// Block shell metacharacters in restricted mode (defense-in-depth)
		for _, arg := range fields[1:] {
			if containsShellMeta(arg) {
				return &ToolResult{Content: fmt.Sprintf("shell metacharacters not allowed in arguments: %q", arg), IsError: true}, nil
			}
		}
	}

	timeout := s.cfg.TimeoutSeconds
	if params.Timeout > 0 {
		timeout = params.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	zap.S().Debugw("executing shell command", "command", truncateCommand(params.Command))

	// Build the command:
	// - Restricted mode (allowed_commands set): exec.Command directly, no shell
	// - Default (no whitelist): use platform-native shell for pipes, redirects, etc.
	var cmd *exec.Cmd
	if len(allowed) > 0 {
		// Restricted: exec.Command with args, no shell
		if len(fields) > 1 {
			cmd = exec.CommandContext(ctx, fields[0], fields[1:]...)
		} else {
			cmd = exec.CommandContext(ctx, fields[0])
		}
	} else {
		// Default: use shell for full shell feature support
		cmd = shellCommand(ctx, params.Command)
	}
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

	const maxOutput = 64 * 1024 // 64KB limit to prevent OOM
	outStr := stdout.String()
	errStr := stderr.String()
	if len(outStr) > maxOutput {
		outStr = outStr[:maxOutput] + "\n... [truncated]"
	}
	if len(errStr) > maxOutput {
		errStr = errStr[:maxOutput] + "\n... [truncated]"
	}
	return &ToolResult{
		Content: fmt.Sprintf("stdout:\n%s\nstderr:\n%s", outStr, errStr),
	}, nil
}

func (s *ShellTool) isAllowed(cmdName string) bool {
	// Normalize: extract base name to prevent path bypass (e.g. /usr/bin/cat → cat)
	name := filepath.Base(cmdName)
	for _, a := range s.cfg.AllowedCommands {
		if name == a || cmdName == a {
			return true
		}
	}
	return false
}

// containsShellMeta checks if a string contains shell metacharacters that could
// enable injection even in restricted (non-sh) mode.
func containsShellMeta(s string) bool {
	metaChars := []string{";", "|", "&", "$", "`"}
	for _, mc := range metaChars {
		if strings.Contains(s, mc) {
			return true
		}
	}
	return false
}

// truncateCommand sanitizes a shell command for logging:
// truncates to 200 chars and removes newlines.
func truncateCommand(cmd string) string {
	const maxLogLen = 200
	// Replace newlines for single-line logging
	cleaned := strings.ReplaceAll(cmd, "\n", "\\n")
	if len(cleaned) > maxLogLen {
		cleaned = cleaned[:maxLogLen] + "..."
	}
	return cleaned
}
