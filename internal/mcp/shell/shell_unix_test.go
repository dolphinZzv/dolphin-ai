//go:build !windows

package shell

import (
	"context"
	"encoding/json"
	"testing"

	"dolphin/internal/config"
)

func TestShellCommandUnix(t *testing.T) {
	cmd := shellCommand(context.Background(), "echo hello")
	if cmd == nil {
		t.Fatal("shellCommand() returned nil")
	}
	args := cmd.Args
	if len(args) < 3 || args[0] != "sh" || args[1] != "-c" || args[2] != "echo hello" {
		t.Errorf("shellCommand args = %v, want [sh -c echo hello]", args)
	}
}

func TestShellExecuteWithWorkdir(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	cfg.MCP.Shell.AllowUnrestricted = true
	tool := New(cfg)
	ctx := WithWorkdir(context.Background(), "/tmp")
	result, err := tool.Execute(ctx, json.RawMessage(`{"command":"pwd"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestShellExecuteRedirectCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	cfg.MCP.Shell.AllowUnrestricted = true
	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hello | tee /dev/null"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}
