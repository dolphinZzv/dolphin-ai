package mcp

import (
	"context"
	"testing"

	"dolphin/internal/config"
)

func TestNewServerClientEmptyCommand(t *testing.T) {
	_, err := NewServerClient(context.Background(), "test", config.MCPServerConfig{
		Type: "stdio",
	}, nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestNewServerClientUnsupportedType(t *testing.T) {
	_, err := NewServerClient(context.Background(), "test", config.MCPServerConfig{
		Type: "http",
		URL:  "http://example.com",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestNewServerClientProcessNotFound(t *testing.T) {
	_, err := NewServerClient(context.Background(), "test", config.MCPServerConfig{
		Type:    "stdio",
		Command: "nonexistent-binary-xyz",
	}, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestRegistryLoadServersEmpty(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	if err := r.LoadServers(context.Background()); err != nil {
		t.Fatalf("LoadServers with empty config: %v", err)
	}
}

func TestRegistryCloseServersEmpty(t *testing.T) {
	r := NewRegistry(config.DefaultConfig())
	r.CloseServers()
	// Should not panic
}
