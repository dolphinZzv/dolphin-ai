package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevModeFullFlow(t *testing.T) {
	homeDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", homeDir)
	defer os.Setenv("HOME", origHome)

	// Find project root (where mcp.json and skills.json live)
	rootDir := findProjectRoot(t)

	// Dev demo profile
	profile := &CareerProfile{
		Name:        "demo",
		Skills:      []string{"frontend-expert", "backend-golang"},
		MCP:         []string{"browser-preview", "filesystem"},
		Description: "Demo (integration test)",
	}

	// Use local fallback only — no network required
	fetcher := NewRepoFetcher(filepath.Join(homeDir, UserConfigDir, "cache"))
	fetcher.SetLocalDir(rootDir)

	skills, mcp := AugmentWithRepos(profile, []string{"dolphinv/skills"}, []string{"dolphinv/mcp"})

	t.Logf("matched skills: %d, mcp: %d", len(skills), len(mcp))

	if len(skills) == 0 {
		t.Error("expected at least 1 matched skill from local skills.json")
	}
	if len(mcp) == 0 {
		t.Error("expected at least 1 matched mcp server from local mcp.json")
	}

	for _, s := range skills {
		t.Logf("  skill: %s (url=%s)", s.Name, s.URL)
	}
	for _, m := range mcp {
		t.Logf("  mcp:   %s (cmd=%s args=%v)", m.Name, m.Command, m.Args)
	}

	// Apply tools
	if err := ApplyTools(skills, mcp); err != nil {
		t.Fatalf("ApplyTools: %v", err)
	}

	// Verify skills directory
	skillsDir := filepath.Join(homeDir, UserConfigDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skill files created")
	}
	for _, e := range entries {
		t.Logf("  skill file: %s", e.Name())
	}

	// Verify MCP config
	configPath := filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configStr := string(data)
	t.Logf("config.yaml:\n%s", configStr)

	if !strings.Contains(configStr, "browser-preview") {
		t.Error("config should contain browser-preview server")
	}
	if !strings.Contains(configStr, "stdio") {
		t.Error("MCP server should be stdio type")
	}
}

func TestDevModeNoDuplicates(t *testing.T) {
	homeDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", homeDir)
	defer os.Setenv("HOME", origHome)

	rootDir := findProjectRoot(t)

	profile := &CareerProfile{
		Name:        "demo",
		Skills:      []string{"browser-preview", "filesystem"},
		MCP:         []string{"browser-preview", "filesystem"},
		Description: "Demo",
	}

	fetcher := NewRepoFetcher(filepath.Join(homeDir, UserConfigDir, "cache"))
	fetcher.SetLocalDir(rootDir)

	_, mcp := AugmentWithRepos(profile, []string{}, []string{"dolphinv/mcp"})

	// Apply twice — no duplicates in config
	if err := ApplyTools(nil, mcp); err != nil {
		t.Fatalf("ApplyTools 1: %v", err)
	}
	if err := ApplyTools(nil, mcp); err != nil {
		t.Fatalf("ApplyTools 2: %v", err)
	}

	configPath := filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml")
	data, _ := os.ReadFile(configPath)
	count := strings.Count(string(data), "browser-preview")
	if count > 2 { // once in servers map key, once in type/command
		t.Errorf("browser-preview appears %d times, expected no duplicates", count)
	}
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "mcp.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("project root not found")
		}
		dir = parent
	}
}
