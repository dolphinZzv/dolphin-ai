package agent

import (
	"os"
	"path/filepath"
	"testing"

	"dolphin/internal/config"
)

func TestAgentDefLoadNonExistentDir(t *testing.T) {
	defs, err := LoadAgentDefs("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map, got %d entries", len(defs))
	}
}

func TestAgentDefLoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map, got %d entries", len(defs))
	}
}

func TestAgentDefLoadWithInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "test-agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("invalid: [yaml: \n"), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map after invalid yaml, got %d", len(defs))
	}
}

func TestAgentDefLoadValid(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "test-agent")
	os.MkdirAll(agentDir, 0755)
	yamlContent := `role: "A test agent"
tools: ["shell"]
timeout: 60
`
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yamlContent), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(defs))
	}
	def, ok := defs["test-agent"]
	if !ok {
		t.Fatal("agent 'test-agent' not found")
	}
	if def.Role != "A test agent" {
		t.Errorf("Role = %q", def.Role)
	}
	if len(def.Tools) != 1 || def.Tools[0] != "shell" {
		t.Errorf("Tools = %v", def.Tools)
	}
	if def.Workspace == "" {
		t.Error("Workspace should be auto-set")
	}
}

func TestAgentDefLoadSkipsNonDirectories(t *testing.T) {
	dir := t.TempDir()
	// Create a file (not a dir) in the agents directory
	os.WriteFile(filepath.Join(dir, "not-a-dir.yaml"), []byte("role: test"), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map, got %d", len(defs))
	}
}

func TestAgentDefLoadSkipsDirWithoutYAML(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "no-yaml"), 0755)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty map, got %d", len(defs))
	}
}

func TestAgentDir(t *testing.T) {
	path := AgentDir("/base/dir", "my-agent")
	if path != "/base/dir/my-agent" {
		t.Errorf("got %q", path)
	}
}

func TestAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	path := AgentWorkspace(cfg, "my-agent")
	if path != "/workspaces/my-agent" {
		t.Errorf("got %q", path)
	}
}

func TestTempAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	path := TempAgentWorkspace(cfg, "my-agent")
	if path != "/workspaces/temp-my-agent" {
		t.Errorf("got %q", path)
	}
}
