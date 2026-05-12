package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBuilder(projectDir, userDir, systemDir string) *Builder {
	return &Builder{
		projectDir: projectDir,
		userDir:    userDir,
		systemDir:  systemDir,
		statCache:  make(map[string]cachedFile),
	}
}

func TestBuilderDefault(t *testing.T) {
	b := testBuilder(t.TempDir(), t.TempDir(), t.TempDir())
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

	b := testBuilder(dir, t.TempDir(), t.TempDir())
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

	b := testBuilder(projectDir, userDir, t.TempDir())
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

	b := testBuilder(t.TempDir(), userDir, t.TempDir())
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "user rules") {
		t.Error("should fall back to user rules")
	}
}

func TestBuilderNoFiles(t *testing.T) {
	b := testBuilder(t.TempDir(), t.TempDir(), t.TempDir())
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
	if b.projectDir != ".dolphin" {
		t.Errorf("projectDir = %q", b.projectDir)
	}
	if b.systemDir != "/etc/dolphin" {
		t.Errorf("systemDir = %q", b.systemDir)
	}
	if b.statCache == nil {
		t.Error("statCache should be initialized")
	}
}

func TestStatCacheHit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(path, []byte("cached content"), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())

	// First read: cache miss, populates cache
	content, ok := b.loadCached(path)
	if !ok || content != "cached content" {
		t.Fatalf("first read: ok=%v content=%q", ok, content)
	}

	// Second read: cache hit (same mtime)
	content2, ok2 := b.loadCached(path)
	if !ok2 || content2 != "cached content" {
		t.Fatalf("second read (cached): ok=%v content=%q", ok2, content2)
	}
}

func TestStatCacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "RULES.md")
	os.WriteFile(path, []byte("v1"), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())

	// Prime cache
	b.loadCached(path)

	// Overwrite the file
	os.WriteFile(path, []byte("v2"), 0644)

	// Should see new content (mtime changed)
	content, ok := b.loadCached(path)
	if !ok || content != "v2" {
		t.Fatalf("after overwrite: ok=%v content=%q", ok, content)
	}
}

func TestLoadCachedNotExist(t *testing.T) {
	b := testBuilder(t.TempDir(), t.TempDir(), t.TempDir())
	content, ok := b.loadCached("/nonexistent/context/file.md")
	if ok || content != "" {
		t.Errorf("expected ok=false, got ok=%v content=%q", ok, content)
	}
}
