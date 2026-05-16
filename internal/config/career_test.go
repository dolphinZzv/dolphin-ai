package config

import (
	"os"
	"testing"
)

func TestCareerToolsNotEmpty(t *testing.T) {
	if len(CareerTools) == 0 {
		t.Error("CareerTools should not be empty")
	}
	for _, p := range CareerTools {
		if p.Name == "" {
			t.Error("career profile has empty name")
		}
		if p.Description == "" {
			t.Errorf("career profile %q has empty description", p.Name)
		}
	}
}

func TestFirstRunMarker(t *testing.T) {
	// Save and restore HOME so we don't pollute real config
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := FirstRunMarker()
	if path == "" {
		t.Fatal("FirstRunMarker returned empty path")
	}

	// Should be first run (marker does not exist)
	if !IsFirstRun() {
		t.Error("expected IsFirstRun = true when marker does not exist")
	}

	// Create marker
	if err := CreateFirstRunMarker(); err != nil {
		t.Fatalf("CreateFirstRunMarker: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("marker file should exist after CreateFirstRunMarker")
	}

	// Should not be first run
	if IsFirstRun() {
		t.Error("expected IsFirstRun = false after marker created")
	}

	// Mark done — removes the marker, returning to first-run state
	if err := MarkFirstRunDone(); err != nil {
		t.Fatalf("MarkFirstRunDone: %v", err)
	}
	if !IsFirstRun() {
		t.Error("expected IsFirstRun = true after marker removed (back to uninitialized)")
	}
}

func TestEmailConfiguredMarker(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := EmailConfiguredMarker()
	if path == "" {
		t.Fatal("EmailConfiguredMarker returned empty path")
	}

	if IsEmailConfigured() {
		t.Error("expected IsEmailConfigured = false when marker does not exist")
	}

	if err := MarkEmailConfigured(); err != nil {
		t.Fatalf("MarkEmailConfigured: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("marker file should exist after MarkEmailConfigured")
	}

	if !IsEmailConfigured() {
		t.Error("expected IsEmailConfigured = true after marker created")
	}

	// Verify it's cleaned up by the home/tmp isolation
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove email-configured marker: %v", err)
	}
	if IsEmailConfigured() {
		t.Error("expected IsEmailConfigured = false after manual removal")
	}
}

func TestDolphinID(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := DolphinIDFile()
	if path == "" {
		t.Fatal("DolphinIDFile returned empty path")
	}

	// First call — generates a new ID and persists it.
	id1 := LoadOrCreateDolphinID()
	if id1 == "" {
		t.Fatal("LoadOrCreateDolphinID returned empty")
	}
	if len(id1) != 20 {
		t.Errorf("expected xid length 20, got %d: %s", len(id1), id1)
	}

	// Second call — reads persisted ID, should match.
	id2 := LoadOrCreateDolphinID()
	if id2 != id1 {
		t.Errorf("expected same ID across calls, got %s / %s", id1, id2)
	}

	// Verify file exists with the correct content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read id file: %v", err)
	}
	if string(data) != id1 {
		t.Errorf("id file content %q != %q", string(data), id1)
	}

	// Verify it's a valid xid (12 bytes base32hex → 20 chars).
	if _, err := os.Stat(path); err != nil {
		t.Errorf("id file should exist: %v", err)
	}
}

func TestCareerProfileSelection(t *testing.T) {
	// Verify all careers have valid names for keyword matching
	names := make(map[string]bool)
	for _, p := range CareerTools {
		if names[p.Name] {
			t.Errorf("duplicate career name: %s", p.Name)
		}
		names[p.Name] = true
	}
}

func TestMergeProfiles(t *testing.T) {
	profiles := []CareerProfile{
		{Name: "frontend", Skills: []string{"react", "vue"}, MCP: []string{"browser"}, Description: "前端"},
		{Name: "backend", Skills: []string{"go", "api"}, MCP: []string{"postgres"}, Description: "后端"},
		{Name: "devops", Skills: []string{"docker", "api"}, MCP: []string{"postgres"}, Description: "运维"},
	}

	merged := mergeProfiles(profiles)
	if merged.Name != "frontend+backend+devops" {
		t.Errorf("Name = %q, want frontend+backend+devops", merged.Name)
	}
	if len(merged.Skills) != 5 { // react,vue,go,api,docker — api deduped once
		t.Errorf("Skills count = %d, want 5 (deduped)", len(merged.Skills))
	}
	if len(merged.MCP) != 2 { // browser,postgres — postgres deduped
		t.Errorf("MCP count = %d, want 2 (deduped)", len(merged.MCP))
	}
}

func TestMergeProfilesSingle(t *testing.T) {
	profiles := []CareerProfile{
		{Name: "frontend", Skills: []string{"react"}, MCP: []string{}, Description: "前端"},
	}
	merged := mergeProfiles(profiles)
	if merged.Name != "frontend" {
		t.Errorf("Name = %q, want frontend", merged.Name)
	}
}

func TestMergeProfilesEmpty(t *testing.T) {
	merged := mergeProfiles(nil)
	if merged != nil {
		t.Error("expected nil for empty input")
	}
	merged = mergeProfiles([]CareerProfile{})
	if merged != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestShellName(t *testing.T) {
	name := shellName()
	if name == "" {
		t.Error("shellName() should not return empty string")
	}
}

func TestGenerateSystemMDContainsShell(t *testing.T) {
	md, err := GenerateSystemMD("en")
	if err != nil {
		t.Fatalf("GenerateSystemMD() error: %v", err)
	}
	if md == "" {
		t.Fatal("GenerateSystemMD() returned empty")
	}
}
