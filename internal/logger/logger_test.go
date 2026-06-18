package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"dolphin/internal/config"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	cfg := config.LoadConfigFromMap(map[string]any{
		"log.level":       "debug",
		"log.file":        logFile,
		"log.max_size":    1,
		"log.max_backups": 1,
		"log.max_age":     1,
		"log.compress":    false,
	})

	logger := New(cfg)
	if logger == nil {
		t.Fatal("New() returned nil")
	}

	logger.Info("test message", zap.String("key", "value"))
	logger.Sync()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected log file to have content")
	}
	if !strings.Contains(string(data), "test message") {
		t.Errorf("expected log to contain 'test message', got: %s", data)
	}
	if !strings.Contains(string(data), "key") {
		t.Errorf("expected log to contain 'key', got: %s", data)
	}
}

func TestNewDefaultValues(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.LoadConfigFromMap(map[string]any{
		"log.level": "info",
		"log.file":  filepath.Join(tmpDir, "app.log"),
	})

	logger := New(cfg)
	if logger == nil {
		t.Fatal("New() returned nil")
	}
	logger.Info("default test")
	logger.Sync()
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected zapcore.LevelEnabler
	}{
		{"debug", zapcore.DebugLevel},
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"info", zapcore.InfoLevel},
		{"unknown", zapcore.InfoLevel},
		{"", zapcore.InfoLevel},
		{"INFO", zapcore.InfoLevel},
		{"DEBUG", zapcore.InfoLevel}, // case-sensitive, falls back to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseLevelEnabled(t *testing.T) {
	// Verify that the returned LevelEnabler actually enables the correct level
	infoLevel := parseLevel("info")
	debugLevel := parseLevel("debug")

	// Info level should enable Info and above
	if !infoLevel.Enabled(zapcore.InfoLevel) {
		t.Error("info level should enable InfoLevel")
	}
	if infoLevel.Enabled(zapcore.DebugLevel) {
		t.Error("info level should NOT enable DebugLevel")
	}
	if !infoLevel.Enabled(zapcore.WarnLevel) {
		t.Error("info level should enable WarnLevel")
	}

	// Debug level should enable everything
	if !debugLevel.Enabled(zapcore.DebugLevel) {
		t.Error("debug level should enable DebugLevel")
	}
	if !debugLevel.Enabled(zapcore.InfoLevel) {
		t.Error("debug level should enable InfoLevel")
	}
}

func TestNamed(t *testing.T) {
	root := zap.NewNop()
	child := Named(root, "test-component")

	if child == nil {
		t.Fatal("Named() returned nil")
	}
	if child.Name() != "test-component" {
		t.Errorf("Name = %q, want %q", child.Name(), "test-component")
	}
}

func TestNamedChain(t *testing.T) {
	root := zap.NewNop()
	child := Named(root, "parent")
	grandchild := Named(child, "child")

	if grandchild.Name() != "parent.child" {
		t.Errorf("chained Name = %q, want %q", grandchild.Name(), "parent.child")
	}
}

func TestNamedEmptyName(t *testing.T) {
	root := zap.NewNop()
	child := Named(root, "")
	if child.Name() != "" {
		t.Errorf("Name = %q, want empty string", child.Name())
	}
}
