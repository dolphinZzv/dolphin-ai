package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"dolphinzZ/internal/config"
)

func TestCDPShutdownUninitialized(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := NewCDPTool(cfg)
	tool.Shutdown()
}

func TestCDPBytesToBase64(t *testing.T) {
	result := bytesToBase64([]byte("hello"))
	if result != "aGVsbG8=" {
		t.Errorf("got %q", result)
	}
}

func TestCDPTruncateShort(t *testing.T) {
	if s := truncate("hello", 10); s != "hello" {
		t.Errorf("got %q", s)
	}
}

func TestCDPTruncateLong(t *testing.T) {
	if s := truncate("hello world", 5); s != "hello..." {
		t.Errorf("got %q", s)
	}
}

func TestCDPTruncateEmpty(t *testing.T) {
	if s := truncate("", 5); s != "" {
		t.Errorf("got %q", s)
	}
}

func TestCDPNavigateNoURL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	cfg.MCP.CDP.Enabled = false
	tool := NewCDPTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"navigate"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for navigate without url")
	}
}

func TestCDPClickNoSelector(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := NewCDPTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"click"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for click without selector")
	}
}

func TestCDPUnknownAction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := NewCDPTool(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"invalid"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestCDPInvalidInput(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := NewCDPTool(cfg)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{invalid json}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}
