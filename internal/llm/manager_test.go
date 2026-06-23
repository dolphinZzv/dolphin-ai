package llm

import (
	"context"
	"strings"
	"testing"

	"dolphin/internal/types"
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

func TestManager_QualifiedModelName_StreamConfig(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("volcengine_agent", &mockProvider{
		name: "volcengine_agent",
		models: []ModelConfig{
			{Name: "minimax-m3", Model: "minimax-m3", Stream: false, StreamSet: true},
			{Name: "deepseek-v4-pro", Model: "deepseek-v4-pro", Stream: true},
		},
	})

	// Set active with qualified name — ActiveModel preserves the section
	// prefix so short-name collisions across sections route correctly.
	if err := mgr.SetActiveModel("volcengine_agent/minimax-m3"); err != nil {
		t.Fatalf("SetActiveModel failed: %v", err)
	}
	if mgr.ActiveModel() != "volcengine_agent/minimax-m3" {
		t.Errorf("expected active model 'volcengine_agent/minimax-m3', got '%s'", mgr.ActiveModel())
	}

	// CompleteStream should use Stream: false from the matched ModelConfig.
	// We verify this by checking the request's Stream field through a custom mock.
	var capturedReq LLMRequest
	mgr.providers["volcengine_agent"] = &captureProvider{
		name: "volcengine_agent",
		models: []ModelConfig{
			{Name: "minimax-m3", Model: "minimax-m3", Stream: false, StreamSet: true},
		},
		capture: func(req LLMRequest) {
			capturedReq = req
		},
	}

	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}
	<-ch

	if capturedReq.Stream {
		t.Error("expected Stream=false in captured request, got true")
	}
}

func TestManager_QualifiedModelName_MatchesShortName(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("volcengine_agent", &mockProvider{
		name: "volcengine_agent",
		models: []ModelConfig{
			{Name: "minimax-m3", Model: "minimax-m3", Stream: false, StreamSet: true},
		},
	})

	// Short name — no slash in input, stays short.
	if err := mgr.SetActiveModel("minimax-m3"); err != nil {
		t.Fatalf("SetActiveModel with short name failed: %v", err)
	}
	if mgr.ActiveModel() != "minimax-m3" {
		t.Errorf("expected 'minimax-m3', got '%s'", mgr.ActiveModel())
	}

	// Qualified name — preserves the section prefix to avoid cross-section ambiguity.
	if err := mgr.SetActiveModel("volcengine_agent/minimax-m3"); err != nil {
		t.Fatalf("SetActiveModel with qualified name failed: %v", err)
	}
	if mgr.ActiveModel() != "volcengine_agent/minimax-m3" {
		t.Errorf("expected 'volcengine_agent/minimax-m3', got '%s'", mgr.ActiveModel())
	}
}

type captureProvider struct {
	name    string
	models  []ModelConfig
	capture func(LLMRequest)
}

func (m *captureProvider) Name() string { return m.name }

func (m *captureProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	if m.capture != nil {
		m.capture(req)
	}
	ch := make(chan LLMChunk)
	close(ch)
	return ch, nil
}

func (m *captureProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	return m.models, nil
}

func TestManager_TopP_Forwarding(t *testing.T) {
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", TopP: 0.85, Temperature: 0.9, MaxTokens: 8192},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	var capturedReq LLMRequest
	mgr.providers["openai"] = &captureProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", TopP: 0.85, Temperature: 0.9, MaxTokens: 8192},
		},
		capture: func(req LLMRequest) {
			capturedReq = req
		},
	}

	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}
	<-ch

	if capturedReq.TopP != 0.85 {
		t.Errorf("expected TopP=0.85, got %f", capturedReq.TopP)
	}
	if capturedReq.Temperature != 0.9 {
		t.Errorf("expected Temperature=0.9, got %f", capturedReq.Temperature)
	}
	if capturedReq.MaxTokens != 8192 {
		t.Errorf("expected MaxTokens=8192, got %d", capturedReq.MaxTokens)
	}
}

func TestManager_TopP_Zero_NotForwarded(t *testing.T) {
	// TopP=0 means "not configured", so it should not override the request.
	mgr := NewManager()
	mgr.AddProvider("openai", &mockProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", TopP: 0},
		},
	})
	mgr.SetActiveModel("gpt-4o")

	var capturedReq LLMRequest
	mgr.providers["openai"] = &captureProvider{
		name: "openai",
		models: []ModelConfig{
			{Name: "gpt-4o", Model: "gpt-4o", TopP: 0},
		},
		capture: func(req LLMRequest) {
			capturedReq = req
		},
	}

	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{TopP: 0.5})
	if err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}
	<-ch

	// TopP=0 in ModelConfig should not override the request's 0.5.
	if capturedReq.TopP != 0.5 {
		t.Errorf("expected TopP=0.5 (unchanged), got %f", capturedReq.TopP)
	}
}

// TestCrossSectionModelNameCollision verifies that when two sections (e.g.
// deepseek_anthropic and volcengine_agent) both define the same short model
// name (e.g. "deepseek-v4-flash"), SetActiveModel with a qualified name
// routes to the correct provider — not the one that happened to be loaded
// first in map-iteration order.
func TestCrossSectionModelNameCollision(t *testing.T) {
	mgr := NewManager()

	// deepseek_anthropic/deepseek-v4-flash (anthropic protocol)
	mgr.AddProvider("deepseek_anthropic", &mockProvider{
		name: "deepseek_anthropic",
		models: []ModelConfig{
			{Name: "deepseek-v4-flash", Model: "deepseek-v4-flash", Thinking: true},
		},
	})

	// volcengine_agent/deepseek-v4-flash (openai protocol)
	mgr.AddProvider("volcengine_agent", &mockProvider{
		name: "volcengine_agent",
		models: []ModelConfig{
			{Name: "deepseek-v4-flash", Model: "deepseek-v4-flash"},
		},
	})

	// Qualified name must route to deepseek_anthropic.
	target := "deepseek_anthropic/deepseek-v4-flash"
	if err := mgr.SetActiveModel(target); err != nil {
		t.Fatalf("SetActiveModel(%q): %v", target, err)
	}
	active := mgr.ActiveModel()
	if !strings.Contains(active, "/") || !strings.Contains(active, "deepseek_anthropic") {
		t.Fatalf("expected qualified active model containing 'deepseek_anthropic', got %q", active)
	}

	// resolveModel must point to deepseek_anthropic, not volcengine_agent.
	// Use a helper that we add inline.
	providerName, err := mgr.resolveModel(active)
	if err != nil {
		t.Fatalf("resolveModel(%q): %v", active, err)
	}
	if providerName != "deepseek_anthropic" {
		t.Fatalf("expected provider 'deepseek_anthropic', got %q", providerName)
	}

	// Short name "deepseek-v4-flash" should also still resolve (it picks
	// whichever section was loaded first — that's inherently non-deterministic
	// for the short name, which is why qualified names are recommended).
	if _, err := mgr.resolveModel("deepseek-v4-flash"); err != nil {
		t.Fatalf("short name 'deepseek-v4-flash' should still resolve: %v", err)
	}

	// Model list must contain both qualified entries.
	models, _ := mgr.Models(context.Background())
	hasQualified := make(map[string]bool)
	for _, m := range models {
		if strings.HasSuffix(m.Name, "/deepseek-v4-flash") {
			hasQualified[m.Name] = true
		}
	}
	for _, q := range []string{
		"deepseek_anthropic/deepseek-v4-flash",
		"volcengine_agent/deepseek-v4-flash",
	} {
		if !hasQualified[q] {
			t.Errorf("model list missing %q", q)
		}
	}

	// Verify model configs are preserved through collision renaming.
	ch, err := mgr.CompleteStream(context.Background(), LLMRequest{
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	<-ch
	t.Logf("cross-section collision resolved correctly")
}
