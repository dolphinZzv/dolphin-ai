package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if b.Dir() != dir {
		t.Errorf("expected dir %q, got %q", dir, b.Dir())
	}
}

func TestInitFresh(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify .git exists.
	if _, err := os.Stat(filepath.Join(dir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD not found: %v", err)
	}

	// Verify seed files.
	for _, name := range []string{"introduction.md", "workflow.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("seed file %s not found: %v", name, err)
		}
	}

	// Verify subdirectory index files.
	for _, name := range []string{"rules/index.md", "knowledge/index.md", "meta/index.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("seed index %s not found: %v", name, err)
		}
	}

	// Verify .gitignore.
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(data), "*.log") {
		t.Errorf(".gitignore should contain *.log")
	}
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}
	// Second init should succeed (open existing repo).
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	content, err := b.Read(context.Background(), "introduction.md")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !strings.Contains(content, "Dolphin") {
		t.Errorf("expected intro to contain Dolphin, got: %s", content)
	}
}

func TestReadNotFound(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := b.Read(context.Background(), "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestWriteAndRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	content := "# Test\n\nHello world."
	if err := b.Write(ctx, "test.md", "", content); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := b.Read(ctx, "test.md")
	if err != nil {
		t.Fatalf("Read after write failed: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestWriteSubdirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := b.Write(ctx, "rules/code-style.md", "", "# Code Style\n\nUse Go."); err != nil {
		t.Fatalf("Write to subdir failed: %v", err)
	}

	got, err := b.Read(ctx, "rules/code-style.md")
	if err != nil {
		t.Fatalf("Read from subdir failed: %v", err)
	}
	if !strings.Contains(got, "Code Style") {
		t.Errorf("unexpected content: %s", got)
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Write additional files.
	b.Write(ctx, "rules/code-style.md", "", "# Code Style")
	b.Write(ctx, "knowledge/glossary.md", "", "# Glossary")

	files, err := b.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should include seed files + new files, no .git entries.
	for _, f := range files {
		if strings.HasPrefix(f, ".git") {
			t.Errorf("list should not include .git: %s", f)
		}
	}

	// Verify expected files exist.
	expected := map[string]bool{
		"introduction.md":       false,
		"workflow.md":           false,
		"rules/index.md":        false,
		"knowledge/index.md":    false,
		"meta/index.md":         false,
		"rules/code-style.md":   false,
		"knowledge/glossary.md": false,
	}
	for _, f := range files {
		if _, ok := expected[f]; ok {
			expected[f] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected file in list: %s", name)
		}
	}
}

func TestGitLog(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	entries, err := b.GitLog(ctx, 5)
	if err != nil {
		t.Fatalf("GitLog failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one commit")
	}
	if entries[0].Message != "chore: init brain" {
		t.Errorf("expected init commit message, got %q", entries[0].Message)
	}
	if entries[0].Hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestGitLogAfterWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	b.Write(ctx, "test.md", "", "content")

	entries, err := b.GitLog(ctx, 5)
	if err != nil {
		t.Fatalf("GitLog failed: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 commits, got %d", len(entries))
	}
}

func TestPathTraversal(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := b.Read(context.Background(), "../outside.txt")
	if err == nil {
		t.Fatal("expected path traversal error")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("expected path traversal error, got: %v", err)
	}

	err = b.Write(context.Background(), "../../outside.txt", "", "content")
	if err == nil {
		t.Fatal("expected path traversal error on write")
	}
}

func TestGitAccessDenied(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	tests := []string{
		".git/config",
		".git/HEAD",
		".git/objects/pack/xxx",
		"subdir/../../.git/config",
	}

	for _, path := range tests {
		_, err := b.Read(ctx, path)
		if err == nil {
			t.Errorf("expected error reading .git path %q", path)
			continue
		}
		if !strings.Contains(err.Error(), ".git access denied") &&
			!strings.Contains(err.Error(), "path traversal denied") {
			t.Errorf("expected .git or path traversal error for %q, got: %v", path, err)
		}
	}

	for _, path := range tests {
		err := b.Write(ctx, path, "", "content")
		if err == nil {
			t.Errorf("expected error writing .git path %q", path)
			continue
		}
		if !strings.Contains(err.Error(), ".git access denied") &&
			!strings.Contains(err.Error(), "path traversal denied") {
			t.Errorf("expected .git or path traversal error for %q, got: %v", path, err)
		}
	}
}

func TestReadIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	idx, err := b.ReadIndex(ctx)
	if err != nil {
		t.Fatalf("ReadIndex failed: %v", err)
	}

	// Should contain root-level introduction content.
	if !strings.Contains(idx, "Brain Index") {
		t.Errorf("expected root index in output, got: %s", idx)
	}

	// Should contain subdirectory index.md content.
	if !strings.Contains(idx, "rules") || !strings.Contains(idx, "knowledge") {
		t.Errorf("expected subdirectory references in index, got: %s", idx)
	}
}

func TestResolveMarkDownFile_modelOverride(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Write base file.
	if err := b.Write(ctx, "DESIGN.md", "", "# Default Design"); err != nil {
		t.Fatalf("write base: %v", err)
	}
	// Write model-specific override.
	if err := b.Write(ctx, "DESIGN@claude-sonnet-4-6.md", "", "# Claude Design"); err != nil {
		t.Fatalf("write override: %v", err)
	}

	got, err := b.ResolveMarkDownFile(ctx, "DESIGN.md", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("ResolveMarkDownFile failed: %v", err)
	}
	if !strings.Contains(got, "Claude Design") {
		t.Errorf("expected model override content, got: %s", got)
	}
}

func TestResolveMarkDownFile_fallbackToBase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := b.Write(ctx, "DESIGN.md", "", "# Default Design"); err != nil {
		t.Fatalf("write base: %v", err)
	}

	// Model override doesn't exist — should fall back.
	got, err := b.ResolveMarkDownFile(ctx, "DESIGN.md", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("ResolveMarkDownFile failed: %v", err)
	}
	if !strings.Contains(got, "Default Design") {
		t.Errorf("expected fallback content, got: %s", got)
	}
}

func TestResolveMarkDownFile_noModelName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := b.Write(ctx, "DESIGN.md", "", "# Default Design"); err != nil {
		t.Fatalf("write base: %v", err)
	}

	got, err := b.ResolveMarkDownFile(ctx, "DESIGN.md", "")
	if err != nil {
		t.Fatalf("ResolveMarkDownFile failed: %v", err)
	}
	if !strings.Contains(got, "Default Design") {
		t.Errorf("expected base content for empty model, got: %s", got)
	}
}

func TestResolveMarkDownFile_bothMissing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := b.ResolveMarkDownFile(ctx, "NONEXISTENT.md", "some-model")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveMarkDownFile_modelSpecificOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Only model-specific file exists, no base.
	if err := b.Write(ctx, "CONFIG@deepseek-v4.md", "", "# Deepseek Config"); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	got, err := b.ResolveMarkDownFile(ctx, "CONFIG.md", "deepseek-v4")
	if err != nil {
		t.Fatalf("ResolveMarkDownFile failed: %v", err)
	}
	if !strings.Contains(got, "Deepseek Config") {
		t.Errorf("expected model content, got: %s", got)
	}
}

func TestInsertModelSuffix(t *testing.T) {
	tests := []struct {
		base, model, expected string
	}{
		{"DESIGN.md", "claude-sonnet-4-6", "DESIGN@claude-sonnet-4-6.md"},
		{"workflow.md", "gpt-4o", "workflow@gpt-4o.md"},
		{"rules/code-style.md", "deepseek-v4", "rules/code-style@deepseek-v4.md"},
	}
	for _, tt := range tests {
		got := insertModelSuffix(tt.base, tt.model)
		if got != tt.expected {
			t.Errorf("insertModelSuffix(%q, %q) = %q, want %q", tt.base, tt.model, got, tt.expected)
		}
	}
}

func TestReadIndexNoGit(t *testing.T) {
	// ReadIndex should work even without git (e.g. during pipeline init sequence).
	dir := t.TempDir()
	b := New(dir)

	// Create files manually without Init.
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "index.md"), []byte("Root index"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "index.md"), []byte("Sub index"), 0644)

	idx, err := b.ReadIndex(context.Background())
	if err != nil {
		t.Fatalf("ReadIndex failed: %v", err)
	}
	if !strings.Contains(idx, "Root index") || !strings.Contains(idx, "Sub index") {
		t.Errorf("unexpected index content: %s", idx)
	}
}
