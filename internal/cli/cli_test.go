package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/zap"
)

func TestScan_empty(t *testing.T) {
	dir := t.TempDir()
	clis := Scan([]string{dir}, zap.NewNop())
	if len(clis) != 0 {
		t.Errorf("expected 0 clis, got %d", len(clis))
	}
}

func TestScan_nonexistentDir(t *testing.T) {
	clis := Scan([]string{"/nonexistent/path"}, zap.NewNop())
	if len(clis) != 0 {
		t.Errorf("expected 0 clis for nonexistent dir, got %d", len(clis))
	}
}

func TestScan_findsExecutables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "demo")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	clis := Scan([]string{dir}, zap.NewNop())
	if len(clis) != 1 {
		t.Fatalf("expected 1 cli, got %d", len(clis))
	}
	if clis[0].Name != "demo" {
		t.Errorf("Name = %q, want 'demo'", clis[0].Name)
	}
	if clis[0].Path != script {
		t.Errorf("Path = %q, want %q", clis[0].Path, script)
	}
}

func TestScan_skipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	clis := Scan([]string{dir}, zap.NewNop())
	if len(clis) != 0 {
		t.Errorf("expected 0 clis for non-executable file, got %d", len(clis))
	}
}

func TestScan_skipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	clis := Scan([]string{dir}, zap.NewNop())
	if len(clis) != 0 {
		t.Errorf("expected 0 clis, got %d", len(clis))
	}
}

func TestScan_laterDirOverrides(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir1, "tool"), []byte("#!/bin/sh\necho v1"), 0o755)
	_ = os.WriteFile(filepath.Join(dir2, "tool"), []byte("#!/bin/sh\necho v2"), 0o755)

	clis := Scan([]string{dir1, dir2}, zap.NewNop())
	if len(clis) != 1 {
		t.Fatalf("expected 1 cli, got %d", len(clis))
	}
	if clis[0].Path != filepath.Join(dir2, "tool") {
		t.Errorf("expected path from dir2 (later overrides), got %s", clis[0].Path)
	}
}

func TestFetchHelp_caches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "demo")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho Usage: demo '<args>'"), 0o755)

	c := CLI{Name: "demo", Path: script}
	FetchHelp(&c, zap.NewNop())

	if c.Help == "" {
		t.Fatal("expected non-empty help")
	}
	if c.Help != "Usage: demo <args>\n" {
		t.Errorf("Help = %q", c.Help)
	}

	// Second call should use cache.
	prev := c.Help
	FetchHelp(&c, zap.NewNop())
	if c.Help != prev {
		t.Error("second FetchHelp should not modify cached Help")
	}
}

func TestFetchHelp_noHelpFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "bad")
	// A script that doesn't support --help, -h, or help — just exits 0.
	_ = os.WriteFile(script, []byte("#!/bin/sh\ntrue"), 0o755)

	c := CLI{Name: "bad", Path: script}
	FetchHelp(&c, zap.NewNop())

	if c.Help != "" {
		t.Errorf("expected empty help for script with no help flags, got %q", c.Help)
	}
}
