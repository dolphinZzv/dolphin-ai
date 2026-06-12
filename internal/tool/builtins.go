package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"dolphin/internal/types"
)

// BuiltinMCPHandlers returns handlers for builtin MCP tools.
// binDirs are prepended to PATH when executing shell commands (may be nil).
func BuiltinMCPHandlers(binDirs []string) map[string]BuiltinHandler {
	return map[string]BuiltinHandler{
		"shell": shellHandler(binDirs),
	}
}

// BuiltinMCPDescriptions returns descriptions for builtin MCP tools.
func BuiltinMCPDescriptions() map[string]string {
	return map[string]string{
		"shell": "Execute a shell command and get the output",
	}
}

// BuiltinMCPSchemas returns JSON schemas for builtin MCP tools.
func BuiltinMCPSchemas() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"shell": json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"number","description":"timeout in seconds"}},"required":["command"]}`),
	}
}

func shellHandler(binDirs []string) BuiltinHandler {
	return func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Command string  `json:"command"`
			Timeout float64 `json:"timeout"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Command == "" {
			return &types.ToolResult{Content: "command is required", IsError: true}, nil
		}

		execCtx := context.Background()
		var cancel func()
		if req.Timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout*float64(time.Second)))
			defer cancel()
		}

		cmd := exec.CommandContext(execCtx, "sh", "-c", req.Command)
		if len(binDirs) > 0 {
			extra := strings.Join(binDirs, ":")
			cmd.Env = append(os.Environ(), "PATH="+extra+":"+os.Getenv("PATH"))
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return &types.ToolResult{
				Content: fmt.Sprintf("error: %v\noutput: %s", err, string(out)),
				IsError: true,
			}, nil
		}
		return &types.ToolResult{Content: string(out)}, nil
	}
}
