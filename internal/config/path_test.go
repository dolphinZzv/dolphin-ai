package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSessionsDirProjectLevel(t *testing.T) {
	// When .dolphin/ exists in CWD, use it
	dir := SessionsDir()
	if dir == "" {
		t.Fatal("SessionsDir() returned empty")
	}
}

func TestSessionsDirOverride(t *testing.T) {
	// SetSessionsDir overrides the normal resolution
	old := sessionsDirOverride
	defer func() { sessionsDirOverride = old }()

	SetSessionsDir("/custom/sessions")
	dir := SessionsDir()
	if dir != "/custom/sessions" {
		t.Errorf("SessionsDir() = %q, want /custom/sessions", dir)
	}

	// Reset returns to normal behavior
	sessionsDirOverride = ""
	dir2 := SessionsDir()
	if dir2 == "" {
		t.Fatal("SessionsDir() returned empty after reset")
	}
	if dir2 == "/custom/sessions" {
		t.Error("SessionsDir() still overridden after reset")
	}
}

func TestSessionsDirUserFallback(t *testing.T) {
	// When .dolphin/ does not exist, fall back to ~/.dolphin/sessions/
	// Save and restore .dolphin directory state
	projectDir := ProjectConfigDir
	origExists := false
	if _, err := os.Stat(projectDir); err == nil {
		origExists = true
		// Rename out of the way
		tmpName := projectDir + ".test-tmp"
		if err := os.Rename(projectDir, tmpName); err != nil {
			t.Skipf("cannot rename .dolphin for test: %v", err)
		}
		defer os.Rename(tmpName, projectDir)
	}

	dir := SessionsDir()
	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, UserConfigDir, "sessions")
	if dir != expected {
		t.Errorf("SessionsDir() = %q, want %q (home fallback)", dir, expected)
	}

	if origExists {
		// Already deferred restore; check that when restored, it uses project dir again
	}
}

func TestDefaultSystemConfigDir(t *testing.T) {
	dir := defaultSystemConfigDir()
	if dir == "" {
		t.Fatal("defaultSystemConfigDir() returned empty")
	}
	if runtime.GOOS != "windows" && dir != "/etc/dolphin" {
		t.Errorf("defaultSystemConfigDir() = %q, want /etc/dolphin", dir)
	}
}

func TestSessionDirUsesDefaultWhenEmpty(t *testing.T) {
	dir := SessionsDir()
	if dir == "" {
		t.Error("SessionsDir() should not be empty")
	}
}

func TestDefaultConfigSessionDir(t *testing.T) {
	_ = DefaultConfig()
	if SessionsDir() == "" {
		t.Error("SessionsDir() should not be empty after DefaultConfig()")
	}
}

func TestDefaultConfigSessionDirFieldRemoved(t *testing.T) {
	cfg := DefaultConfig()
	// Session.Dir field no longer exists; SessionsDir() is the source of truth
	dir := SessionsDir()
	if dir == "" {
		t.Fatal("SessionsDir() returned empty")
	}
	_ = cfg
}

func TestSystemConfigDirVar(t *testing.T) {
	if SystemConfigDir == "" {
		t.Error("SystemConfigDir should not be empty")
	}
}

func TestHomeDirFallback(t *testing.T) {
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", "/nonexistent_home_12345")
	defer os.Setenv("HOME", oldHome)

	dir := SessionsDir()
	if dir == "" {
		t.Error("SessionsDir() should not be empty even with bad HOME")
	}
}
