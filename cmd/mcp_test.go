package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMCPTest creates an isolated test environment for MCP commands.
// It writes the config to .dolphin/config.yaml inside dir so that both
// config.Load(cfgFile) and resolveConfigDir() (used by RemoveMCPServer
// and ToggleMCPServer) can find it.
func setupMCPTest(t *testing.T, servers map[string]map[string]any) (origCfg string, dir string) {
	t.Helper()
	origCfg = cfgFile
	dir = t.TempDir()

	origWd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() {
		os.Chdir(origWd)
		cfgFile = origCfg
	})

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	// Write config to .dolphin/config.yaml so resolveConfigDir() finds it
	dolphinDir := filepath.Join(dir, ".dolphin")
	os.MkdirAll(dolphinDir, 0700)

	var sb strings.Builder
	sb.WriteString("session:\n  dir: " + dir + "/sessions\n")
	sb.WriteString("skills:\n  dir: " + dir + "/skills\n  repos: []\n")
	sb.WriteString("llm:\n  api_key: test-key\n  model: test-model\n  type: openai\n  base_url: http://localhost:9999\n")
	sb.WriteString("mcp:\n  repos: [\"org/mcp-repo\"]\n  shell:\n    enabled: false\n  cdp:\n    enabled: false\n")

	sb.WriteString("  servers:\n")
	for name, srv := range servers {
		sb.WriteString("    " + name + ":\n")
		for k, v := range srv {
			switch val := v.(type) {
			case string:
				sb.WriteString("      " + k + ": " + val + "\n")
			case bool:
				sb.WriteString("      " + k + ": " + map[bool]string{true: "true", false: "false"}[val] + "\n")
			case int:
				sb.WriteString("      " + k + ": " + itoa(val) + "\n")
			}
		}
	}

	configPath := filepath.Join(dolphinDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cfgFile = configPath
	return
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestNewMCPCmd_HasSubcommands(t *testing.T) {
	cmd := NewMCPCmd()
	subs := cmd.Commands()
	expected := map[string]bool{"search": false, "install": false, "uninstall": false, "enable": false, "disable": false}
	for _, sub := range subs {
		if _, ok := expected[sub.Name()]; !ok {
			t.Errorf("unexpected subcommand: %s", sub.Name())
		}
		expected[sub.Name()] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestMCPList_Empty(t *testing.T) {
	setupMCPTest(t, nil)

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "No MCP servers") {
		t.Errorf("expected empty message, got: %s", output)
	}
}

func TestMCPList_WithServers(t *testing.T) {
	setupMCPTest(t, map[string]map[string]any{
		"server-a": {"type": "stdio", "command": "tool-a"},
		"server-b": {"type": "stdio", "command": "tool-b"},
	})

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "server-a") {
		t.Errorf("expected server-a in output, got: %s", output)
	}
	if !strings.Contains(output, "server-b") {
		t.Errorf("expected server-b in output, got: %s", output)
	}
	if !strings.Contains(output, "Total: 2 MCP servers") {
		t.Errorf("expected total count, got: %s", output)
	}
}

func TestMCPEnable(t *testing.T) {
	setupMCPTest(t, map[string]map[string]any{
		"server-a": {"type": "stdio", "command": "tool-a", "enabled": false},
	})

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"enable", "server-a"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "enabled") {
		t.Errorf("expected enabled message, got: %s", output)
	}
}

func TestMCPEnable_NotFound(t *testing.T) {
	setupMCPTest(t, nil)

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"enable", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestMCPDisable(t *testing.T) {
	setupMCPTest(t, map[string]map[string]any{
		"server-a": {"type": "stdio", "command": "tool-a"},
	})

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"disable", "server-a"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "disabled") {
		t.Errorf("expected disabled message, got: %s", output)
	}
}

func TestMCPDisable_NotFound(t *testing.T) {
	setupMCPTest(t, nil)

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"disable", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestMCPUninstall(t *testing.T) {
	setupMCPTest(t, map[string]map[string]any{
		"server-a": {"type": "stdio", "command": "tool-a"},
	})

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"uninstall", "server-a"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "uninstalled") {
		t.Errorf("expected uninstalled message, got: %s", output)
	}
}

func TestMCPUninstall_NotFound(t *testing.T) {
	setupMCPTest(t, nil)

	cmd := NewMCPCmd()
	cmd.SetArgs([]string{"uninstall", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent server")
	}
}
