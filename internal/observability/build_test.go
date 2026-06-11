package observability

import (
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/hook"
)

func TestOTelHeaders(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{})
		headers := otelHeaders(cfg)
		if len(headers) != 0 {
			t.Fatalf("expected 0 headers, got %d", len(headers))
		}
	})

	t.Run("returns configured headers", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{
			"otel.headers.Authorization": "Bearer token123",
			"otel.headers.X-Custom":      "custom-value",
		})
		headers := otelHeaders(cfg)
		if len(headers) != 2 {
			t.Fatalf("expected 2 headers, got %d", len(headers))
		}
		if headers["Authorization"] != "Bearer token123" {
			t.Fatalf("expected 'Bearer token123', got '%s'", headers["Authorization"])
		}
		if headers["X-Custom"] != "custom-value" {
			t.Fatalf("expected 'custom-value', got '%s'", headers["X-Custom"])
		}
	})

	t.Run("case-sensitive key preservation", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{
			"otel.headers.authorization": "lowcase",
			"otel.headers.Authorization": "upcase",
		})
		headers := otelHeaders(cfg)
		// Both are valid distinct keys due to case-sensitive header names.
		if headers["authorization"] != "lowcase" {
			t.Fatalf("expected 'lowcase', got '%s'", headers["authorization"])
		}
		if headers["Authorization"] != "upcase" {
			t.Fatalf("expected 'upcase', got '%s'", headers["Authorization"])
		}
	})
}

func TestBuildObservability_Disabled(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"otel.enabled": false,
	})
	hr := hook.NewRegistry()
	shutdown := BuildObservability(cfg, hr)
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
	shutdown() // should not panic
}

func TestBuildObservability_EnabledNoEndpoint(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"otel.enabled":       true,
		"llm.use":       "openai",
		"llm.openai.api_key": "test",
	})
	hr := hook.NewRegistry()
	// Without endpoint, the exporter creation will fail, but BuildObservability
	// should return a no-op shutdown rather than crashing.
	shutdown := BuildObservability(cfg, hr)
	shutdown() // should not panic
}
