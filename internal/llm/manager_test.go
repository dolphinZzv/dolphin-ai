package llm

import (
	"context"
	"testing"
)

type mockProvider struct {
	name   string
	models []ModelConfig
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk)
	close(ch)
	return ch, nil
}

func (m *mockProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	return m.models, nil
}

func TestManager_AddProviderAndModels(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", Provider: "openai"},
			{Name: "gpt-4o-mini", Model: "gpt-4o-mini", Provider: "openai"},
		},
	})
	mgr.AddProvider("anthropic", &mockProvider{
		name: "anthropic",
		models: []ModelConfig{
			{Name: "claude-sonnet-4-6", Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	})

	models, err := mgr.Models(context.Background())
	if err != nil {
		t.Fatalf("Models failed: %v", err)
	}
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}
}

func TestManager_ModelCollision(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o"},
		},
	})
	mgr.AddProvider("custom", &mockProvider{
		name: "custom",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o"},
		},
	})

	// Both models should be discoverable via qualified names.
	_, err := mgr.resolveModel("openai/gpt-4o")
	if err != nil {
		t.Errorf("expected openai/gpt-4o to resolve: %v", err)
	}
	_, err = mgr.resolveModel("custom/gpt-4o")
	if err != nil {
		t.Errorf("expected custom/gpt-4o to resolve: %v", err)
	}
}

func TestManager_ActiveModel(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o"},
		},
	})

	if err := mgr.SetActiveModel("gpt-4o"); err != nil {
		t.Fatalf("SetActiveModel failed: %v", err)
	}
	if mgr.ActiveModel() != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", mgr.ActiveModel())
	}

	// Unknown model should error.
	if err := mgr.SetActiveModel("nonexistent"); err == nil {
		t.Error("expected error for unknown model")
	}
}

func TestManager_RoutingByModelName(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o"},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	// Route with explicit model name.
	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}
	<-ch // drain

	// Route with empty model name (uses active).
	ch, err = mgr.CompleteStream(context.Background(), LLMRequest{Model: ""})
	if err != nil {
		t.Fatalf("CompleteStream with empty model failed: %v", err)
	}
	<-ch

	// Route with unknown model should error.
	_, err = mgr.CompleteStream(context.Background(), LLMRequest{Model: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown model")
	}
}

func TestManager_NoProvider(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.CompleteStream(context.Background(), LLMRequest{Model: "gpt-4o"})
	if err == nil {
		t.Error("expected error when no providers registered")
	}
}

func TestManager_Name(t *testing.T) {
	mgr := NewManager()
	if mgr.Name() != "manager" {
		t.Errorf("Name = %q", mgr.Name())
	}
}

func TestManager_MaxConcurrency(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", MaxConcurrency: 2},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	// With MaxConcurrency set, CompleteStream should still succeed.
	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("CompleteStream with concurrency failed: %v", err)
	}
	<-ch // drain

	// Verify semaphore was created.
	if _, ok := mgr.semaphores["gpt-4o"]; !ok {
		t.Error("expected semaphore to be created for model with MaxConcurrency")
	}
}

func TestManager_MaxConcurrencyContextCancel(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", MaxConcurrency: 1},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	// Fill the semaphore.
	sem := mgr.getSemaphore("gpt-4o", 1)
	sem <- struct{}{}

	// Now the semaphore is full. A context-canceled request should return ctx.Err().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mgr.CompleteStream(ctx, LLMRequest{Model: "gpt-4o"})
	if err == nil {
		t.Error("expected error for canceled context with full semaphore")
	}

	// Drain the semaphore.
	<-sem
}

func TestManager_ConcurrentLimitsSequentialAccess(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", MaxConcurrency: 1},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	// First call acquires the semaphore.
	ch1, err := mgr.CompleteStream(context.Background(), LLMRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	<-ch1

	// After draining, the semaphore should be available again.
	ch2, err := mgr.CompleteStream(context.Background(), LLMRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	<-ch2
}
