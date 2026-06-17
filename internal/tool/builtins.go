package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"dolphin/internal/progress"
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

		execCtx := ctx
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

		// Stream stdout+stderr line-by-line into a buffer, feeding the
		// watchdog on each line. A long-running command that emits
		// progress (build, test suite) keeps the turn alive naturally;
		// a silent stall (sleep 300, hung network) is correctly detected
		// by the idle watchdog. Feed is nil-safe and throttled, so
		// per-line calls are cheap.
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		if err := cmd.Start(); err != nil {
			pw.Close()
			return &types.ToolResult{
				Content: fmt.Sprintf("error: %v", err),
				IsError: true,
			}, nil
		}

		var buf bytes.Buffer
		copyDone := make(chan struct{})
		go func() {
			defer close(copyDone)
			reader := bufio.NewReader(pr)
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					buf.WriteString(line)
					progress.Feed(ctx)
				}
				if err != nil {
					if err != io.EOF {
						fmt.Fprintf(&buf, "\n[read error: %v]\n", err)
					}
					break
				}
			}
		}()

		waitErr := cmd.Wait()
		pw.Close()
		<-copyDone

		if waitErr != nil {
			return &types.ToolResult{
				Content: fmt.Sprintf("error: %v\noutput: %s", waitErr, buf.String()),
				IsError: true,
			}, nil
		}
		return &types.ToolResult{Content: buf.String()}, nil
	}
}
