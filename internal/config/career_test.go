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
