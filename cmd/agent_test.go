package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/i18n"
)

func init() {
	i18n.SetLang(i18n.EN)
}

// setupAgentTest creates an isolated test environment for agent CLI commands.
// Uses "test/local-agents" as repo name so GitHub fetch fails and local fallback is used.
func setupAgentTest(t *testing.T, agents []map[string]string) (dir string) {
	t.Helper()
	origCfg := cfgFile
	dir = t.TempDir()

	origWd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() {
		os.Chdir(origWd)
		cfgFile = origCfg
	})

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	dolphinDir := filepath.Join(dir, ".dolphin")
	os.MkdirAll(dolphinDir, 0700)

	var sb strings.Builder
	sb.WriteString("session:\n  dir: " + dir + "/sessions\n")
	sb.WriteString("skills:\n  dir: " + dir + "/skills\n  repos: []\n")
	sb.WriteString("agents:\n  repos:\n    - test/local-agents\n")
	sb.WriteString("llm:\n  api_key: test-key\n  model: test-model\n  type: openai\n  base_url: http://localhost:9999\n")
	sb.WriteString("mcp:\n  shell:\n    enabled: false\n  cdp:\n    enabled: false\n")

	configPath := filepath.Join(dolphinDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cfgFile = configPath

	// Write local-agents.json as local fallback for "test/local-agents" repo.
	// localManifestName("test/local-agents") → "local-agents.json"
	if agents != nil {
		manifest := map[string]interface{}{
			"version":     "1.0",
			"description": "test agents",
			"agents":      agents,
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "local-agents.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	return
}

func TestAgentSearch_Found(t *testing.T) {
	agents := []map[string]string{
		{"name": "reviewer", "description": "code review expert", "url": "git@github.com:dolphinZzv/demo_agent.git"},
	}
	setupAgentTest(t, agents)

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"search", "review"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "reviewer") {
		t.Errorf("search should find 'reviewer', got: %s", output)
	}
	if !strings.Contains(output, "Found") {
		t.Errorf("search output should contain 'Found', got: %s", output)
	}
}

func TestAgentSearch_ByDescription(t *testing.T) {
	agents := []map[string]string{
		{"name": "reviewer", "description": "code review expert", "url": "git@github.com:dolphinZzv/demo_agent.git"},
	}
	setupAgentTest(t, agents)

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"search", "code review"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "reviewer") {
		t.Errorf("search by description should find 'reviewer', got: %s", output)
	}
}

func TestAgentSearch_NotFound(t *testing.T) {
	agents := []map[string]string{
		{"name": "reviewer", "description": "reviewer", "url": "git@github.com:dolphinZzv/demo_agent.git"},
	}
	setupAgentTest(t, agents)

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"search", "nonexistent"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "No agents found") {
		t.Errorf("search should report no results, got: %s", output)
	}
}

func TestAgentInstall_FromLocalPath(t *testing.T) {
	dir := setupAgentTest(t, nil)

	// Create a local agent source directory (simulates the demo_agent repo content)
	agentSrc := filepath.Join(dir, "agent-src")
	os.MkdirAll(agentSrc, 0700)
	agentYAML := []byte("name: reviewer\nrole: |\n  You are a code review expert.\ntools:\n  - shell\ntimeout: 120\n")
	if err := os.WriteFile(filepath.Join(agentSrc, "agent.yaml"), agentYAML, 0600); err != nil {
		t.Fatal(err)
	}

	// Write local-agents.json with local path URL pointing to agent-src
	agents := []map[string]string{
		{"name": "reviewer", "description": "reviewer", "url": agentSrc},
	}
	manifest := map[string]interface{}{
		"version":     "1.0",
		"description": "test agents",
		"agents":      agents,
	}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "local-agents.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"install", "reviewer"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "installed successfully") {
		t.Errorf("expected install success, got: %s", output)
	}

	// Verify agent.yaml was copied to .dolphin/agents/reviewer/
	installedYAML := filepath.Join(dir, ".dolphin", "agents", "reviewer", "agent.yaml")
	if _, err := os.Stat(installedYAML); os.IsNotExist(err) {
		t.Errorf("agent.yaml should exist at %s after install", installedYAML)
	}

	// Verify search now shows it as installed (* marker)
	searchCmd := NewAgentCmd()
	searchCmd.SetArgs([]string{"search", "reviewer"})
	searchOutput := captureOutput(func() {
		if err := searchCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(searchOutput, "*") {
		t.Errorf("installed agent should show '*' marker in search, got: %s", searchOutput)
	}
}

func TestAgentInstall_AlreadyInstalled(t *testing.T) {
	dir := setupAgentTest(t, nil)

	// Pre-create the agent directory with agent.yaml (simulating already installed)
	agentDir := filepath.Join(dir, ".dolphin", "agents", "reviewer")
	os.MkdirAll(agentDir, 0700)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("name: reviewer\nrole: test\n"), 0600)

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"install", "reviewer"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for already installed agent")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected 'already installed' error, got: %v", err)
	}
}

func TestAgentInstall_NotFound(t *testing.T) {
	setupAgentTest(t, nil)

	cmd := NewAgentCmd()
	cmd.SetArgs([]string{"install", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
	if !strings.Contains(err.Error(), "not found in any configured repo") {
		t.Errorf("expected 'not found in any configured repo' error, got: %v", err)
	}
}
