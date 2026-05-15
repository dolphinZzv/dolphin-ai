package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"dolphin/internal/config"
)

func TestWithWorkdir(t *testing.T) {
	ctx := context.Background()
	ctx2 := WithWorkdir(ctx, "/tmp/test")
	if ctx2 == ctx {
		t.Error("expected different context")
	}
}

func TestShellExecuteInvalidInput(t *testing.T) {
	tool := NewShellTool(config.DefaultConfig())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestShellExecuteEmptyCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = []string{"echo"}
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":""}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty command")
	}
}

func TestShellExecuteDisallowedCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = []string{"echo"}
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for disallowed command")
	}
}

func TestShellExecuteSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestShellDefinition(t *testing.T) {
	tool := NewShellTool(config.DefaultConfig())
	def := tool.Definition()
	if def.Name != "shell" {
		t.Errorf("Name = %q", def.Name)
	}
	if def.InputSchema == nil {
		t.Error("InputSchema is nil")
	}
}

func TestShellExecuteWithWorkdir(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	tool := NewShellTool(cfg)
	ctx := WithWorkdir(context.Background(), "/tmp")
	result, err := tool.Execute(ctx, json.RawMessage(`{"command":"pwd"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestShellExecuteTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	cfg.MCP.Shell.TimeoutSeconds = 1
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep 5"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected timeout error")
	}
}

func TestShellExecutePipeCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = nil
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hello | wc -c"}`))
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
	tool := NewShellTool(cfg)
	// Use a pipe to test shell features: echo piped to cat with redirect target
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hello | tee /dev/null"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestShellExecuteRestrictedMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.Shell.AllowedCommands = []string{"echo", "ls"}
	tool := NewShellTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}
