package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilderDefault(t *testing.T) {
	b := &Builder{
		projectDir: t.TempDir(),
		userDir:    t.TempDir(),
		systemDir:  t.TempDir(),
	}
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if result == "" {
		t.Fatal("Build returned empty string")
	}
	// Should include the default preface
	if !strings.Contains(result, "Dolphin") {
		t.Error("result should contain Dolphin from preface")
	}
}

func TestBuilderLoadsFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("test agents"), 0644)
	os.WriteFile(filepath.Join(dir, "RULES.md"), []byte("test rules"), 0644)
	os.WriteFile(filepath.Join(dir, "USER.md"), []byte("test user"), 0644)

	b := &Builder{
		projectDir: dir,
		userDir:    t.TempDir(),
		systemDir:  t.TempDir(),
	}
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "test agents") {
		t.Error("result should contain agents content")
	}
	if !strings.Contains(result, "test rules") {
		t.Error("result should contain rules content")
	}
	if !strings.Contains(result, "test user") {
		t.Error("result should contain user content")
	}
}

func TestBuilderProjectOverridesUser(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("project agents"), 0644)
	os.WriteFile(filepath.Join(userDir, "AGENTS.md"), []byte("user agents"), 0644)

	b := &Builder{
		projectDir: projectDir,
		userDir:    userDir,
		systemDir:  t.TempDir(),
	}
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "project agents") {
		t.Error("should use project agents")
	}
	if strings.Contains(result, "user agents") {
		t.Error("should NOT use user agents when project exists")
	}
}

func TestBuilderUserFallback(t *testing.T) {
	userDir := t.TempDir()
	os.WriteFile(filepath.Join(userDir, "RULES.md"), []byte("user rules"), 0644)

	b := &Builder{
		projectDir: t.TempDir(), // no files here
		userDir:    userDir,
		systemDir:  t.TempDir(),
	}
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "user rules") {
		t.Error("should fall back to user rules")
	}
}

func TestBuilderNoFiles(t *testing.T) {
	b := &Builder{
		projectDir: t.TempDir(),
		userDir:    t.TempDir(),
		systemDir:  t.TempDir(),
	}
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	// Should still have the preface
	if !strings.Contains(result, "Dolphin") {
		t.Error("result should contain preface")
	}
}

func TestNewBuilder(t *testing.T) {
	b := NewBuilder()
	if b == nil {
		t.Fatal("NewBuilder returned nil")
	}
	if b.projectDir != ".dolphinzZ" {
		t.Errorf("projectDir = %q", b.projectDir)
	}
	if b.systemDir != "/etc/dolphinzZ" {
		t.Errorf("systemDir = %q", b.systemDir)
	}
}
