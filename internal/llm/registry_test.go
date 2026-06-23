package llm

import (
	"testing"

	"go.uber.org/zap"
)

func TestRegisterModelProvider_Lookup(t *testing.T) {
	key := "stub-model/openai"
	RegisterModelProvider(key, func(cfg Config, logger *zap.Logger) Provider { return nil })
	defer delete(modelFactories, key)

	f, err := LookupModelProvider("stub-model", "openai")
	if err != nil {
		t.Fatalf("lookup error: %v", err)
	}
	if f == nil {
		t.Fatal("expected factory")
	}
}

func TestLookupModelProvider_MissingIsError(t *testing.T) {
	if _, err := LookupModelProvider("no-such-model", "openai"); err == nil {
		t.Fatal("expected error for unregistered model, got nil (no silent fallback)")
	}
}

func TestRegisteredModelProviders_IncludesRegistered(t *testing.T) {
	key := "diag-model/openai"
	RegisterModelProvider(key, func(cfg Config, logger *zap.Logger) Provider { return nil })
	defer delete(modelFactories, key)

	found := false
	for _, k := range RegisteredModelProviders() {
		if k == key {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected %q in registered providers: %v", key, RegisteredModelProviders())
	}
}
