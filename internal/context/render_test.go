package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dolphin/internal/config"
)

func TestNestConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Name = "test-bot"
	cfg.LLM.Model = "gpt-4o"

	m := nestConfig(cfg)
	if m == nil {
		t.Fatal("nestConfig returned nil")
	}
	if m["name"] != "test-bot" {
		t.Errorf("name = %q, want test-bot", m["name"])
	}
	llm, ok := m["llm"].(map[string]any)
	if !ok {
		t.Fatal("llm should be a nested map")
	}
	if llm["model"] != "gpt-4o" {
		t.Errorf("llm.model = %q, want gpt-4o", llm["model"])
	}
	if _, ok := llm["max_tokens"]; !ok {
		t.Error("llm.max_tokens should exist in nested map")
	}
}

func TestNewRenderData(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Name = "test-bot"

	rd := NewRenderData(cfg)
	if rd == nil {
		t.Fatal("NewRenderData returned nil")
	}
	if rd.Config["name"] != "test-bot" {
		t.Errorf("Config.name = %q, want test-bot", rd.Config["name"])
	}
	if rd.Env == nil {
		t.Error("Env should not be nil")
	}
}

func TestNewRenderDataNil(t *testing.T) {
	rd := NewRenderData(nil)
	if rd != nil {
		t.Error("NewRenderData(nil) should return nil")
	}
}

func TestExpandTemplateBasic(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{
			"name": "dolphin",
			"id":   "abc123",
			"llm": map[string]any{
				"model": "gpt-4o",
			},
		},
		Env: map[string]string{
			"HOME": "/home/test",
		},
	}

	result := expandTemplate("test", "Name: {{.Config.name}}, Model: {{.Config.llm.model}}", data)
	if result != "Name: dolphin, Model: gpt-4o" {
		t.Errorf("got %q", result)
	}
}

func TestExpandTemplateDefaultFallback(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{default .Config.name "dolphin"}}`, data)
	if result != "dolphin" {
		t.Errorf("default fallback: got %q, want dolphin", result)
	}
}

func TestExpandTemplateOrFallback(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{or .Config.name "dolphin"}}`, data)
	if result != "dolphin" {
		t.Errorf("or fallback: got %q, want dolphin", result)
	}
}

func TestExpandTemplateEnvFunction(t *testing.T) {
	os.Setenv("DZ_TEST_VAR", "hello")
	defer os.Unsetenv("DZ_TEST_VAR")

	data := &RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{env "DZ_TEST_VAR"}}`, data)
	if result != "hello" {
		t.Errorf("env function: got %q, want hello", result)
	}
}

func TestExpandTemplateEnvWithFallback(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{or (env "NONEXISTENT_VAR") "default"}}`, data)
	if result != "default" {
		t.Errorf("env fallback: got %q, want default", result)
	}
}

func TestExpandTemplateConditional(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{"id": "abc123"},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{if .Config.id}}ID: {{.Config.id}}{{else}}no id{{end}}`, data)
	if result != "ID: abc123" {
		t.Errorf("conditional: got %q", result)
	}
}

func TestExpandTemplateEmptyID(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	}

	result := expandTemplate("test", `{{if .Config.id}}ID: {{.Config.id}}{{else}}no id{{end}}`, data)
	if result != "no id" {
		t.Errorf("conditional empty: got %q", result)
	}
}

func TestExpandTemplateParseError(t *testing.T) {
	data := &RenderData{
		Config: map[string]any{"name": "dolphin"},
		Env:    map[string]string{},
	}

	// Unclosed action — should return raw content
	raw := "Hello {{.Config.name"
	result := expandTemplate("bad", raw, data)
	if result != raw {
		t.Errorf("parse error: got %q, want raw %q", result, raw)
	}
}

func TestExpandTemplateNilData(t *testing.T) {
	raw := "Hello {{.Config.name}}"
	result := expandTemplate("test", raw, nil)
	if result != raw {
		t.Errorf("nil data: got %q, want raw %q", result, raw)
	}
}

func TestBuilderWithTemplateExpansion(t *testing.T) {
	dir := t.TempDir()

	// Write an AGENTS.md with template syntax
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(
		"You are {{default .Config.name \"dolphin\"}}, instance {{or .Config.id \"unknown\"}}.",
	), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())
	b.SetRenderData(&RenderData{
		Config: map[string]any{"name": "test-bot", "id": "xid123"},
		Env:    map[string]string{},
	})

	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "You are test-bot, instance xid123.") {
		t.Errorf("template not expanded: %s", result)
	}
}

func TestBuilderWithTemplateDefaultFallback(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "RULES.md"), []byte(
		`Name: {{default .Config.name "dolphin"}}, Model: {{default .Config.llm.model "unknown"}}`,
	), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())
	// Don't set name or model — should use defaults
	b.SetRenderData(&RenderData{
		Config: map[string]any{},
		Env:    map[string]string{},
	})

	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "Name: dolphin, Model: unknown") {
		t.Errorf("defaults not applied: %s", result)
	}
}

func TestBuilderWithoutRenderData(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("{{.Config.name}}"), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())
	// No SetRenderData — template should NOT be expanded
	result, err := b.Build()
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !strings.Contains(result, "{{.Config.name}}") {
		t.Error("without render data, template should be left raw")
	}
}

func TestStatCacheTemplateReExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(path, []byte("Name: {{.Config.name}}"), 0644)

	b := testBuilder(dir, t.TempDir(), t.TempDir())
	b.SetRenderData(&RenderData{
		Config: map[string]any{"name": "v1"},
		Env:    map[string]string{},
	})

	// First read
	content, ok := b.loadCached(path)
	if !ok || content != "Name: v1" {
		t.Fatalf("first read: ok=%v content=%q", ok, content)
	}

	// Change render data and re-read — should hit cache, NOT re-expand
	b.SetRenderData(&RenderData{
		Config: map[string]any{"name": "v2"},
		Env:    map[string]string{},
	})
	content2, ok2 := b.loadCached(path)
	if !ok2 || content2 != "Name: v1" {
		t.Errorf("cache should return v1: ok=%v content=%q", ok2, content2)
	}
}
