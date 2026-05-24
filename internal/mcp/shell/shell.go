package shell

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
	"dolphin/internal/mcp"

	"go.uber.org/zap"
)

// workdirKey is used to pass a working directory through context for workspace isolation.
type workdirKey struct{}

// WithWorkdir returns a context that sets the working directory for shell tool execution.
func WithWorkdir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workdirKey{}, dir)
}

// Tool implements shell command execution via MCP.
type Tool struct {
	cfg    *config.ShellConfig
	schema json.RawMessage
}

func New(cfg *config.Config) *Tool {
	if len(cfg.MCP.Shell.AllowedCommands) == 0 && !cfg.MCP.Shell.AllowUnrestricted {
		zap.S().Warnw("shell tool: allow_unrestricted=false has no effect when allowed_commands is empty. " +
			"Set allowed_commands to restrict commands, or remove allow_unrestricted=false to suppress this warning.")
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

	return &Tool{
		cfg:    &cfg.MCP.Shell,
		schema: schema,
	}
}

func (s *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "shell",
		Description: "Execute a shell command and return its output. Use this for file operations, running scripts, and interacting with the system.",
		InputSchema: s.schema,
		Priority:    s.cfg.Priority,
		Source:      "built-in",
	}
}

func (s *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	maxLen := s.cfg.MaxCommandLength
	if maxLen <= 0 {
		maxLen = 4096
	}
	if len(params.Command) > maxLen {
		return &mcp.ToolResult{Content: fmt.Sprintf("command too long (%d bytes, max %d)", len(params.Command), maxLen), IsError: true}, nil
	}

	allowed := s.cfg.AllowedCommands
	fields := strings.Fields(params.Command)
	if len(fields) == 0 {
		return &mcp.ToolResult{Content: "empty command", IsError: true}, nil
	}

	// When allow_unrestricted is true, allowed_commands is ignored
	// and the command runs directly via sh -c.
	if s.cfg.AllowUnrestricted {
		return s.executeUnrestricted(ctx, params.Command, params.Timeout)
	}

	if len(allowed) > 0 {
		if !s.isAllowed(fields[0]) {
			return &mcp.ToolResult{Content: fmt.Sprintf("command not allowed: %s (allowed: %v)", fields[0], allowed), IsError: true}, nil
		}
		for _, arg := range fields[1:] {
			if containsShellMeta(arg) {
				return &mcp.ToolResult{Content: fmt.Sprintf("shell metacharacters not allowed in arguments: %q", arg), IsError: true}, nil
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

	var cmd *exec.Cmd
	if len(allowed) > 0 {
		if len(fields) > 1 {
			cmd = exec.CommandContext(ctx, fields[0], fields[1:]...)
		} else {
			cmd = exec.CommandContext(ctx, fields[0])
		}
	} else {
		// allow_unrestricted=false and no allowed_commands — restricted
		return &mcp.ToolResult{
			Content: "shell restricted: configure mcp.shell.allowed_commands or set mcp.shell.allow_unrestricted=true",
			IsError: true,
		}, nil
	}
	if wd, ok := ctx.Value(workdirKey{}).(string); ok && wd != "" {
		cmd.Dir = wd
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return &mcp.ToolResult{
				Content: fmt.Sprintf("command timed out after %ds:\nstdout:\n%s\nstderr:\n%s", timeout, stdout.String(), stderr.String()),
				IsError: true,
			}, nil
		}
		return &mcp.ToolResult{
			Content: fmt.Sprintf("command failed:\nstdout:\n%s\nstderr:\n%s\nerror: %v", stdout.String(), stderr.String(), err),
			IsError: true,
		}, nil
	}

	maxOutput := s.cfg.OutputMaxBytes
	if maxOutput <= 0 {
		maxOutput = 64 * 1024
	}
	outStr := stdout.String()
	errStr := stderr.String()
	if len(outStr) > maxOutput {
		outStr = outStr[:maxOutput] + "\n... [truncated]"
	}
	if len(errStr) > maxOutput {
		errStr = errStr[:maxOutput] + "\n... [truncated]"
	}
	return &mcp.ToolResult{
		Content: fmt.Sprintf("stdout:\n%s\nstderr:\n%s", outStr, errStr),
	}, nil
}

// executeUnrestricted runs the command via sh -c with no allowlist checks.
// Used when AllowUnrestricted is true.
func (s *Tool) executeUnrestricted(ctx context.Context, command string, timeoutSec int) (*mcp.ToolResult, error) {
	timeout := s.cfg.TimeoutSeconds
	if timeoutSec > 0 {
		timeout = timeoutSec
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	zap.S().Debugw("executing unrestricted shell command", "command", truncateCommand(command))

	cmd := shellCommand(ctx, command)
	if wd, ok := ctx.Value(workdirKey{}).(string); ok && wd != "" {
		cmd.Dir = wd
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return &mcp.ToolResult{
				Content: fmt.Sprintf("command timed out after %ds:\nstdout:\n%s\nstderr:\n%s", timeout, stdout.String(), stderr.String()),
				IsError: true,
			}, nil
		}
		return &mcp.ToolResult{
			Content: fmt.Sprintf("command failed:\nstdout:\n%s\nstderr:\n%s\nerror: %v", stdout.String(), stderr.String(), err),
			IsError: true,
		}, nil
	}

	maxOutput := s.cfg.OutputMaxBytes
	if maxOutput <= 0 {
		maxOutput = 64 * 1024
	}
	outStr := stdout.String()
	errStr := stderr.String()
	if len(outStr) > maxOutput {
		outStr = outStr[:maxOutput] + "\n... [truncated]"
	}
	if len(errStr) > maxOutput {
		errStr = errStr[:maxOutput] + "\n... [truncated]"
	}
	return &mcp.ToolResult{
		Content: fmt.Sprintf("stdout:\n%s\nstderr:\n%s", outStr, errStr),
	}, nil
}

func (s *Tool) isAllowed(cmdName string) bool {
	name := filepath.Base(cmdName)
	for _, a := range s.cfg.AllowedCommands {
		if name == a || cmdName == a {
			return true
		}
	}
	return false
}

func containsShellMeta(s string) bool {
	metaChars := []string{";", "|", "&", "$", "`"}
	for _, mc := range metaChars {
		if strings.Contains(s, mc) {
			return true
		}
	}
	return false
}

func truncateCommand(cmd string) string {
	const maxLogLen = 200
	cleaned := strings.ReplaceAll(cmd, "\n", "\\n")
	if len(cleaned) > maxLogLen {
		cleaned = cleaned[:maxLogLen] + "..."
	}
	return cleaned
}
