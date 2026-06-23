package models

import (
	"testing"

	"dolphin/internal/llm"
)

func TestMergedHeaders(t *testing.T) {
	t.Run("nil when neither set", func(t *testing.T) {
		cfg := llm.Config{}
		mc := llm.ModelConfig{}
		if got := mergedHeaders(cfg, mc); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("section only", func(t *testing.T) {
		cfg := llm.Config{Headers: map[string]string{"X-Org": "acme"}}
		mc := llm.ModelConfig{}
		got := mergedHeaders(cfg, mc)
		if got["X-Org"] != "acme" {
			t.Errorf("X-Org = %q, want %q", got["X-Org"], "acme")
		}
	})

	t.Run("model only", func(t *testing.T) {
		cfg := llm.Config{}
		mc := llm.ModelConfig{Headers: map[string]string{"X-Model-Version": "v2"}}
		got := mergedHeaders(cfg, mc)
		if got["X-Model-Version"] != "v2" {
			t.Errorf("X-Model-Version = %q, want %q", got["X-Model-Version"], "v2")
		}
	})

	t.Run("model overrides section", func(t *testing.T) {
		cfg := llm.Config{Headers: map[string]string{
			"X-Org":   "acme",
			"X-Route": "default",
		}}
		mc := llm.ModelConfig{Headers: map[string]string{
			"X-Route": "experimental",
			"X-Extra": "model-only",
		}}
		got := mergedHeaders(cfg, mc)
		if got["X-Org"] != "acme" {
			t.Errorf("X-Org (section) should be preserved, got %q", got["X-Org"])
		}
		if got["X-Route"] != "experimental" {
			t.Errorf("X-Route should be overridden by model, got %q", got["X-Route"])
		}
		if got["X-Extra"] != "model-only" {
			t.Errorf("X-Extra (model-only) should be present, got %q", got["X-Extra"])
		}
	})
}
