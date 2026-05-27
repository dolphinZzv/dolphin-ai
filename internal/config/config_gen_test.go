package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/i18n"
)

func TestGenerateConfigFileEN(t *testing.T) {
	// Override ProjectConfigDir to a temp dir
	orig := ProjectConfigDir
	ProjectConfigDir = t.TempDir()
	defer func() { ProjectConfigDir = orig }()

	path, err := GenerateConfigFile(i18n.EN)
	if err != nil {
		t.Fatalf("GenerateConfigFile EN: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# dolphin configuration") {
		t.Error("expected English header comment")
	}
	if !strings.Contains(content, "llm:") {
		t.Error("expected llm section")
	}
	if !strings.Contains(content, "agent_pool:") {
		t.Error("expected agent_pool section")
	}
	if !strings.Contains(content, "transport:") {
		t.Error("expected transport section")
	}
	if !strings.Contains(content, "max_pending_result_len") {
		t.Error("expected max_pending_result_len field")
	}
}

func TestGenerateConfigFileZH(t *testing.T) {
	orig := ProjectConfigDir
	ProjectConfigDir = t.TempDir()
	defer func() { ProjectConfigDir = orig }()

	path, err := GenerateConfigFile(i18n.ZH)
	if err != nil {
		t.Fatalf("GenerateConfigFile ZH: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# dolphin 配置文件") {
		t.Error("expected Chinese header comment")
	}
	if !strings.Contains(content, "llm:") {
		t.Error("expected llm section")
	}
	if !strings.Contains(content, "LLM 提供商") {
		t.Error("expected Chinese section comment for LLM")
	}
	if !strings.Contains(content, "传输层") {
		t.Error("expected Chinese section comment for transport")
	}
}

func TestGenerateConfigFileOverwrites(t *testing.T) {
	orig := ProjectConfigDir
	ProjectConfigDir = t.TempDir()
	defer func() { ProjectConfigDir = orig }()

	path := filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("old content"), 0644)

	_, err := GenerateConfigFile(i18n.EN)
	if err != nil {
		t.Fatalf("GenerateConfigFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "old content") {
		t.Error("GenerateConfigFile should overwrite existing file")
	}
}

func TestGenerateConfigFileCreatesDir(t *testing.T) {
	orig := ProjectConfigDir
	ProjectConfigDir = filepath.Join(t.TempDir(), "nested", "config")
	defer func() { ProjectConfigDir = orig }()

	_, err := GenerateConfigFile(i18n.EN)
	if err != nil {
		t.Fatalf("GenerateConfigFile should create dirs: %v", err)
	}
}

func TestGenerateConfigFileValidYAML(t *testing.T) {
	orig := ProjectConfigDir
	ProjectConfigDir = t.TempDir()
	defer func() { ProjectConfigDir = orig }()

	path, err := GenerateConfigFile(i18n.EN)
	if err != nil {
		t.Fatalf("GenerateConfigFile: %v", err)
	}

	// Set env var so provider validation passes (template has empty api_key)
	t.Setenv("DZ_LLM_API_KEY", "sk-test-key")

	// Verify the file is loadable by the existing Load function
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load generated config: %v", err)
	}
	if cfg.LLM.Model != "claude-sonnet-4-6" {
		t.Errorf("expected model claude-sonnet-4-6, got %q", cfg.LLM.Model)
	}
	if cfg.Pool.MaxConcurrency != 5 {
		t.Errorf("expected MaxConcurrency 5, got %d", cfg.Pool.MaxConcurrency)
	}
	if cfg.Transport.Stdio.Enabled != true {
		t.Error("expected stdio enabled")
	}
}

func TestGenerateRestrictiveConfigFile(t *testing.T) {
	orig := ProjectConfigDir
	ProjectConfigDir = t.TempDir()
	defer func() { ProjectConfigDir = orig }()

	path, err := GenerateRestrictiveConfigFile(i18n.EN)
	if err != nil {
		t.Fatalf("GenerateRestrictiveConfigFile: %v", err)
	}

	// Set env var so provider validation passes (template has empty api_key)
	t.Setenv("DZ_LLM_API_KEY", "sk-test-key")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load restrictive config: %v", err)
	}

	if len(cfg.MCP.Shell.AllowedCommands) == 0 {
		t.Error("restrictive: expected non-empty allowed_commands")
	}
	if cfg.MCP.CDP.Enabled {
		t.Error("restrictive: expected CDP disabled")
	}
	if cfg.MCP.Webhook.Enabled {
		t.Error("restrictive: expected webhook disabled")
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("restrictive: expected log_level warn, got %q", cfg.Log.Level)
	}
	if cfg.Plugins.Enabled {
		t.Error("restrictive: expected plugins disabled")
	}
}
