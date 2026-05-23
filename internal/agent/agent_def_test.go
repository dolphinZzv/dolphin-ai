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
	//nolint:gocritic
	if path != filepath.Join("/base/dir", "my-agent") {
		t.Errorf("got %q", path)
	}
}

func TestAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	path := AgentWorkspace(cfg, "my-agent")
	//nolint:gocritic
	if path != filepath.Join("/workspaces", "my-agent") {
		t.Errorf("got %q", path)
	}
}

func TestTempAgentWorkspace(t *testing.T) {
	cfg := &config.PoolConfig{WorkspaceDir: "/workspaces"}
	path := TempAgentWorkspace(cfg, "my-agent")
	//nolint:gocritic
	if path != filepath.Join("/workspaces", "temp-my-agent") {
		t.Errorf("got %q", path)
	}
}

func TestAgentDefLoadWithSkillsWorkflows(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "reviewer")
	os.MkdirAll(agentDir, 0755)
	yamlContent := `role: "Code reviewer"
tools: ["shell"]
skills:
  - code-review
  - security-scan
workflows:
  - review-flow
timeout: 120
`
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yamlContent), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	def, ok := defs["reviewer"]
	if !ok {
		t.Fatal("agent 'reviewer' not found")
	}

	if len(def.Skills) != 2 || def.Skills[0] != "code-review" || def.Skills[1] != "security-scan" {
		t.Errorf("Skills = %v, want [code-review security-scan]", def.Skills)
	}
	if len(def.Workflows) != 1 || def.Workflows[0] != "review-flow" {
		t.Errorf("Workflows = %v, want [review-flow]", def.Workflows)
	}
}

func TestAgentDefLoadDefaultsSkillsWorkflowsEmpty(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "default-agent")
	os.MkdirAll(agentDir, 0755)
	yamlContent := `role: "Default agent"
tools: ["shell"]
`
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yamlContent), 0644)

	defs, err := LoadAgentDefs(dir)
	if err != nil {
		t.Fatalf("LoadAgentDefs error: %v", err)
	}
	def, ok := defs["default-agent"]
	if !ok {
		t.Fatal("agent 'default-agent' not found")
	}

	if def.Skills != nil {
		t.Errorf("Skills should be nil when not specified, got %v", def.Skills)
	}
	if def.Workflows != nil {
		t.Errorf("Workflows should be nil when not specified, got %v", def.Workflows)
	}
}
