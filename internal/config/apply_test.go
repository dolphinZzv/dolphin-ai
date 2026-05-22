package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRemoveMCPServer(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
				},
				"server-b": map[string]any{
					"type":    "stdio",
					"command": "tool-b",
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	if err := RemoveMCPServer("server-a"); err != nil {
		t.Fatalf("RemoveMCPServer: %v", err)
	}

	// Verify server-a is removed
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, exists := cfg.MCP.Servers["server-a"]; exists {
		t.Error("server-a should be removed")
	}
	if _, exists := cfg.MCP.Servers["server-b"]; !exists {
		t.Error("server-b should still exist")
	}
}

func TestRemoveMCPServer_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	err := RemoveMCPServer("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestRemoveMCPServer_NoServersSection(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	err := RemoveMCPServer("any")
	if err == nil {
		t.Fatal("expected error when no servers section")
	}
}

func TestToggleMCPServer_Enable(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
					"enabled": false,
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	if err := ToggleMCPServer("server-a", true); err != nil {
		t.Fatalf("ToggleMCPServer(true): %v", err)
	}

	// Read the raw file to verify
	full := readTestConfig(t, dir)
	enabled := full["mcp"].(map[string]any)["servers"].(map[string]any)["server-a"].(map[string]any)["enabled"]
	if enabled != true {
		t.Errorf("enabled = %v, want true", enabled)
	}
}

func TestToggleMCPServer_Disable(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	// Toggle to disabled
	if err := ToggleMCPServer("server-a", false); err != nil {
		t.Fatalf("ToggleMCPServer(false): %v", err)
	}

	// Read the raw file to verify
	full := readTestConfig(t, dir)
	enabled := full["mcp"].(map[string]any)["servers"].(map[string]any)["server-a"].(map[string]any)["enabled"]
	if enabled != false {
		t.Errorf("enabled = %v, want false", enabled)
	}
}

func TestToggleMCPServer_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	err := ToggleMCPServer("nonexistent", true)
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestToggleMCPServer_PreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestConfig(t, dir, map[string]any{
		"mcp": map[string]any{
			"repos": []string{"org/repo"},
			"servers": map[string]any{
				"server-a": map[string]any{
					"type":    "stdio",
					"command": "tool-a",
					"args":    []string{"--flag", "value"},
					"timeout": 60,
				},
				"server-b": map[string]any{
					"type":    "sse",
					"url":     "http://localhost:9090",
					"headers": map[string]any{"Authorization": "Bearer token"},
				},
			},
		},
	})
	orig := ProjectConfigDir
	ProjectConfigDir = dir
	t.Cleanup(func() { ProjectConfigDir = orig })

	// Enable server-a (it has no enabled field yet, so add one)
	if err := ToggleMCPServer("server-a", true); err != nil {
		t.Fatalf("ToggleMCPServer: %v", err)
	}

	// Remove server-b entirely
	if err := RemoveMCPServer("server-b"); err != nil {
		t.Fatalf("RemoveMCPServer: %v", err)
	}

	// Verify remaining config
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// server-a should still exist with its args and timeout
	srvA, exists := cfg.MCP.Servers["server-a"]
	if !exists {
		t.Fatal("server-a should exist")
	}
	if srvA.Command != "tool-a" {
		t.Errorf("command = %q, want 'tool-a'", srvA.Command)
	}
	if len(srvA.Args) != 2 || srvA.Args[0] != "--flag" || srvA.Args[1] != "value" {
		t.Errorf("args = %v, want [--flag value]", srvA.Args)
	}
	if srvA.Timeout != 60 {
		t.Errorf("timeout = %d, want 60", srvA.Timeout)
	}

	// server-b should be removed
	if _, exists := cfg.MCP.Servers["server-b"]; exists {
		t.Error("server-b should be removed")
	}

	// MCP repos should be preserved
	if len(cfg.MCP.Repos) != 1 || cfg.MCP.Repos[0] != "org/repo" {
		t.Errorf("repos = %v, want [org/repo]", cfg.MCP.Repos)
	}
}

// writeMCPTestConfig writes a config.yaml from a map structure for testing.
func writeMCPTestConfig(t *testing.T, dir string, data map[string]any) {
	t.Helper()
	os.MkdirAll(dir, 0755)
	content, err := yaml.Marshal(data)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// readTestConfig reads a config.yaml from dir and returns it as a map.
func readTestConfig(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var full map[string]any
	if err := yaml.Unmarshal(data, &full); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	return full
}
