package setup

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"dolphin/internal/config"
	"dolphin/internal/limit"
	"dolphin/internal/session"
	"dolphin/internal/userio"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type bootStep struct {
	name  string
	index int
	fn    func()
}

func (b *bootStep) Name() string { return b.name }
func (b *bootStep) Index() int   { return b.index }
func (b *bootStep) Bootstrap(_ context.Context, _ *Context) error {
	if b.fn != nil {
		b.fn()
	}
	return nil
}

type bootErr struct {
	name  string
	index int
	err   error
}

func (b *bootErr) Name() string { return b.name }
func (b *bootErr) Index() int   { return b.index }
func (b *bootErr) Bootstrap(_ context.Context, _ *Context) error {
	return b.err
}

// configMap implements the subset of config.Config needed by discoverProviderNames.
type configMap map[string]string

func (m configMap) GetString(key string) string { return m[key] }
func (m configMap) Keys() []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// configMapFull implements GetString, GetInt, GetFloat for parseProviderModels.
type configMapFull struct {
	configMap
	ints   map[string]int
	floats map[string]float64
}

func (m configMapFull) GetInt(key string) int {
	if m.ints == nil {
		return 0
	}
	return m.ints[key]
}

func (m configMapFull) GetFloat(key string) float64 {
	if m.floats == nil {
		return 0
	}
	return m.floats[key]
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.bootstrappers) != 0 {
		t.Errorf("expected empty registry, got %d bootstrappers", len(r.bootstrappers))
	}
}

func TestRegistryRegister(t *testing.T) {
	r := NewRegistry()
	b := &bootStep{name: "test", index: 10}
	r.Register(b)
	if len(r.bootstrappers) != 1 {
		t.Fatalf("expected 1 bootstrapper, got %d", len(r.bootstrappers))
	}
	if r.bootstrappers[0].Name() != "test" {
		t.Errorf("got name %q", r.bootstrappers[0].Name())
	}
}

func TestRegistryBootstrapOrder(t *testing.T) {
	r := NewRegistry()
	var order []string
	r.Register(&bootStep{name: "b", index: 2, fn: func() { order = append(order, "b") }})
	r.Register(&bootStep{name: "a", index: 1, fn: func() { order = append(order, "a") }})
	r.Register(&bootStep{name: "c", index: 3, fn: func() { order = append(order, "c") }})

	err := r.Bootstrap(context.Background(), &Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("wrong order: %v", order)
	}
}

func TestRegistryBootstrapError(t *testing.T) {
	r := NewRegistry()
	var ranOK bool
	r.Register(&bootStep{name: "ok", index: 10, fn: func() { ranOK = true }})
	r.Register(&bootErr{name: "bad", index: 20, err: fmt.Errorf("setup failed")})
	r.Register(&bootStep{name: "never", index: 30, fn: func() { t.Error("should not run") }})

	err := r.Bootstrap(context.Background(), &Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "setup failed") {
		t.Errorf("unexpected error: %v", err)
	}
	if !ranOK {
		t.Error("first bootstrapper should have run")
	}
}

func TestRegistryBootstrapEmpty(t *testing.T) {
	r := NewRegistry()
	err := r.Bootstrap(context.Background(), &Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

func TestNewContext(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{"test": "value"})
	c := NewContext(cfg)
	if c == nil {
		t.Fatal("NewContext returned nil")
	}
	if c.Config != cfg {
		t.Error("Config not set")
	}
	if c.Config.GetString("test") != "value" {
		t.Errorf("unexpected config value: %s", c.Config.GetString("test"))
	}
}

// ---------------------------------------------------------------------------
// Bootstrapper metadata
// ---------------------------------------------------------------------------

func TestLoggerBootstrapper(t *testing.T) {
	b := &LoggerBootstrapper{}
	if b.Name() != "logger" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 10 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestBusesBootstrapper(t *testing.T) {
	b := &BusesBootstrapper{}
	if b.Name() != "buses" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 20 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestSessionBootstrapper(t *testing.T) {
	b := &SessionBootstrapper{}
	if b.Name() != "session" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 30 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestSessionBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session manager when missing", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(map[string]any{"memory": map[string]any{"dir": t.TempDir()}})}
		b := &SessionBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.SessionMgr == nil {
			t.Fatal("SessionMgr should be set")
		}
	})

	t.Run("no-op when SessionMgr already set", func(t *testing.T) {
		mgr := session.NewManager(t.TempDir())
		c := &Context{SessionMgr: mgr}
		b := &SessionBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.SessionMgr != mgr {
			t.Error("SessionMgr should not be replaced")
		}
	})
}

func TestMemoryBootstrapper(t *testing.T) {
	b := &MemoryBootstrapper{}
	if b.Name() != "memory" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 40 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestLimitBootstrapper(t *testing.T) {
	b := &LimitBootstrapper{}
	if b.Name() != "limit" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 45 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestLLMBootstrapper(t *testing.T) {
	b := &LLMBootstrapper{}
	if b.Name() != "llm" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 50 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestToolsBootstrapper(t *testing.T) {
	b := &ToolsBootstrapper{}
	if b.Name() != "tools" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 60 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestBrainBootstrapper(t *testing.T) {
	b := &BrainBootstrapper{}
	if b.Name() != "brain" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 70 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestSchedulerBootstrapper(t *testing.T) {
	b := &SchedulerBootstrapper{}
	if b.Name() != "scheduler" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 80 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestAgentIOBootstrapper(t *testing.T) {
	b := &AgentIOBootstrapper{}
	if b.Name() != "agentio" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 90 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestUserIOBootstrapper(t *testing.T) {
	b := &UserIOBootstrapper{}
	if b.Name() != "userio" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 100 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestUserIOBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("creates userio when missing", func(t *testing.T) {
		c := &Context{
			Config:     config.LoadConfigFromMap(nil),
			SessionMgr: session.NewManager(t.TempDir()),
		}
		b := &UserIOBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.UserIO == nil {
			t.Fatal("UserIO should be set")
		}
	})

	t.Run("no-op when UserIO already set", func(t *testing.T) {
		c := &Context{UserIO: &userio.UserIO{}}
		b := &UserIOBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestObservabilityBootstrapper(t *testing.T) {
	b := &ObservabilityBootstrapper{}
	if b.Name() != "observability" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 110 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestObservabilityBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("creates otel shutdown when missing", func(t *testing.T) {
		buses := &BusesBootstrapper{}
		c := &Context{Config: config.LoadConfigFromMap(nil)}
		err := buses.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("buses bootstrap error: %v", err)
		}

		obs := &ObservabilityBootstrapper{}
		err = obs.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.OtelShutdown == nil {
			t.Error("OtelShutdown should be set")
		}
	})

	t.Run("no-op when OtelShutdown already set", func(t *testing.T) {
		c := &Context{OtelShutdown: func() {}}
		b := &ObservabilityBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTransportsBootstrapper(t *testing.T) {
	b := &TransportsBootstrapper{}
	if b.Name() != "transports" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 120 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestLimitBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("skips when no limits configured", func(t *testing.T) {
		logC := &Context{Config: config.LoadConfigFromMap(nil)}
		logB := &LoggerBootstrapper{}
		err := logB.Bootstrap(ctx, logC)
		if err != nil {
			t.Fatalf("logger bootstrap: %v", err)
		}

		buses := &BusesBootstrapper{}
		c := &Context{Config: config.LoadConfigFromMap(nil), Logger: logC.Logger}
		err = buses.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("buses bootstrap: %v", err)
		}

		b := &LimitBootstrapper{}
		err = b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Limit != nil {
			t.Error("Limit should be nil when no limits configured")
		}
	})

	t.Run("no-op when Limit already set", func(t *testing.T) {
		c := &Context{Limit: &limit.Limiter{}}
		b := &LimitBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Bootstrap methods (simple ones needing minimal deps)
// ---------------------------------------------------------------------------

func TestLoggerBootstrapperBootstrap(t *testing.T) {
	b := &LoggerBootstrapper{}
	ctx := context.Background()
	c := &Context{Config: config.LoadConfigFromMap(map[string]any{"log": map[string]any{"level": "debug"}})}

	err := b.Bootstrap(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Logger == nil {
		t.Fatal("Logger should be set")
	}

	// Second call with Logger already set should be no-op.
	c2 := &Context{Config: c.Config, Logger: c.Logger}
	err = b.Bootstrap(ctx, c2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBusesBootstrapperBootstrap(t *testing.T) {
	b := &BusesBootstrapper{}
	ctx := context.Background()
	c := &Context{Config: config.LoadConfigFromMap(nil)}

	err := b.Bootstrap(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.EventBus == nil {
		t.Error("EventBus should be set")
	}
	if c.HookReg == nil {
		t.Error("HookReg should be set")
	}
	if c.SignalBus == nil {
		t.Error("SignalBus should be set")
	}

	// Second call should be no-op.
	c2 := &Context{EventBus: c.EventBus, HookReg: c.HookReg, SignalBus: c.SignalBus}
	err = b.Bootstrap(ctx, c2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryBootstrapperBootstrap(t *testing.T) {
	b := &MemoryBootstrapper{}
	ctx := context.Background()
	dir := t.TempDir()
	c := &Context{Config: config.LoadConfigFromMap(map[string]any{
		"memory": map[string]any{
			"dir":    dir,
			"window": 100,
		},
	})}

	err := b.Bootstrap(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Mem == nil {
		t.Error("Mem should be set")
	}

	// Second call should be no-op.
	c2 := &Context{Config: c.Config, Mem: c.Mem}
	err = b.Bootstrap(ctx, c2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBusesWithLogger(t *testing.T) {
	b := &BusesBootstrapper{}
	ctx := context.Background()
	logC := &Context{Config: config.LoadConfigFromMap(map[string]any{"log": map[string]any{"level": "debug"}})}
	logB := &LoggerBootstrapper{}
	_ = logB.Bootstrap(ctx, logC)

	c := &Context{Config: logC.Config, Logger: logC.Logger}
	err := b.Bootstrap(ctx, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.EventBus == nil {
		t.Error("EventBus should be set")
	}
}

// ---------------------------------------------------------------------------
// discoverProviderNames
// ---------------------------------------------------------------------------

func TestDiscoverProviderNames(t *testing.T) {
	t.Run("discovers providers from llm.<name>.api_key", func(t *testing.T) {
		cfg := configMap{
			"llm.openai.api_key":    "sk-abc",
			"llm.anthropic.api_key": "sk-xyz",
			"llm.deepseek.api_key":  "sk-123",
			"llm.provider":          "openai",
		}
		names := discoverProviderNames(cfg)
		if len(names) == 0 {
			t.Fatal("expected providers, got none")
		}
		found := make(map[string]bool)
		for _, n := range names {
			found[n] = true
		}
		if !found["openai"] || !found["anthropic"] || !found["deepseek"] {
			t.Errorf("missing providers: %v", names)
		}
	})

	t.Run("returns nil when no providers configured", func(t *testing.T) {
		cfg := configMap{"llm.model": "gpt-4"}
		names := discoverProviderNames(cfg)
		if names != nil {
			t.Errorf("expected nil, got %v", names)
		}
	})

	t.Run("skips non-llm keys", func(t *testing.T) {
		cfg := configMap{"some.api_key": "sk-xyz", "other.api_key": "sk-abc"}
		names := discoverProviderNames(cfg)
		if names != nil {
			t.Errorf("expected nil for non-llm keys, got %v", names)
		}
	})

	t.Run("skips nested llm keys", func(t *testing.T) {
		cfg := configMap{"llm.openai.models.0.api_key": "sk-abc"}
		names := discoverProviderNames(cfg)
		if names != nil {
			t.Errorf("expected nil for nested keys, got %v", names)
		}
	})

	t.Run("puts preferred provider first", func(t *testing.T) {
		cfg := configMap{
			"llm.openai.api_key":    "sk-abc",
			"llm.anthropic.api_key": "sk-xyz",
			"llm.deepseek.api_key":  "sk-123",
			"llm.provider":          "deepseek",
		}
		names := discoverProviderNames(cfg)
		if len(names) < 2 {
			t.Fatalf("expected at least 2 providers, got %d", len(names))
		}
		if names[0] != "deepseek" {
			t.Errorf("expected deepseek first, got %s", names[0])
		}
	})

	t.Run("empty config", func(t *testing.T) {
		cfg := configMap{}
		names := discoverProviderNames(cfg)
		if names != nil {
			t.Errorf("expected nil for empty config, got %v", names)
		}
	})
}

// ---------------------------------------------------------------------------
// isPreferredProvider
// ---------------------------------------------------------------------------

func TestIsPreferredProvider(t *testing.T) {
	t.Run("matches by provider field", func(t *testing.T) {
		cfg := configMap{"llm.openai.provider": "openai"}
		if !isPreferredProvider(cfg, "openai", "openai") {
			t.Error("should match by provider field")
		}
	})

	t.Run("matches by api_type", func(t *testing.T) {
		cfg := configMap{"llm.deepseek.api_type": "anthropic"}
		if !isPreferredProvider(cfg, "deepseek", "anthropic") {
			t.Error("should match by api_type")
		}
	})

	t.Run("matches by section name", func(t *testing.T) {
		cfg := configMap{}
		if !isPreferredProvider(cfg, "claude", "claude") {
			t.Error("should match by section name")
		}
	})

	t.Run("no match", func(t *testing.T) {
		cfg := configMap{}
		if isPreferredProvider(cfg, "openai", "anthropic") {
			t.Error("should not match different names")
		}
	})
}

// ---------------------------------------------------------------------------
// parseProviderModels
// ---------------------------------------------------------------------------

func TestParseProviderModels(t *testing.T) {
	t.Run("parses models from config", func(t *testing.T) {
		cfg := configMapFull{
			configMap: configMap{
				"llm.deepseek.provider":      "deepseek",
				"llm.deepseek.api_type":      "anthropic",
				"llm.deepseek.models.0.name": "deepseek-v4-pro",
			},
			ints:   map[string]int{"llm.deepseek.models.0.max_tokens": 8192},
			floats: map[string]float64{"llm.deepseek.models.0.temperature": 0.7},
		}
		models := parseProviderModels(cfg, "deepseek")
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].Name != "deepseek-v4-pro" {
			t.Errorf("Name = %q", models[0].Name)
		}
		if models[0].Vendor != "deepseek" {
			t.Errorf("Vendor = %q", models[0].Vendor)
		}
		if models[0].APIType != "anthropic" {
			t.Errorf("APIType = %q", models[0].APIType)
		}
		if models[0].MaxTokens != 8192 {
			t.Errorf("MaxTokens = %d", models[0].MaxTokens)
		}
		if models[0].Temperature != 0.7 {
			t.Errorf("Temperature = %f", models[0].Temperature)
		}
	})

	t.Run("returns empty when no models configured", func(t *testing.T) {
		cfg := configMapFull{configMap: configMap{}}
		models := parseProviderModels(cfg, "openai")
		if len(models) != 0 {
			t.Errorf("expected 0 models, got %d", len(models))
		}
	})
}

// ---------------------------------------------------------------------------
// configListOrString
// ---------------------------------------------------------------------------

func TestConfigListOrString(t *testing.T) {
	t.Run("returns single string value", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{"allow_users": "user1"})
		result := configListOrString(cfg, "allow_users")
		if result != "user1" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("joins list values", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{
			"allow_users.0": "user1",
			"allow_users.1": "user2",
			"allow_users.2": "user3",
		})
		result := configListOrString(cfg, "allow_users")
		if result != "user1,user2,user3" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("returns empty when key not found", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(nil)
		result := configListOrString(cfg, "nonexistent")
		if result != "" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("returns empty for empty list", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{"items": map[string]any{}})
		result := configListOrString(cfg, "items")
		if result != "" {
			t.Errorf("got %q", result)
		}
	})
}

// ---------------------------------------------------------------------------
// hasTransportType
// ---------------------------------------------------------------------------

func TestHasTransportType(t *testing.T) {
	tcs := []transportConfig{
		{Type: "stdio"},
		{Type: "dingtalk"},
		{Type: "email"},
	}

	t.Run("finds existing type", func(t *testing.T) {
		if !hasTransportType(tcs, "dingtalk") {
			t.Error("should find dingtalk")
		}
	})

	t.Run("returns false for missing type", func(t *testing.T) {
		if hasTransportType(tcs, "wework") {
			t.Error("should not find wework")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		if hasTransportType(nil, "stdio") {
			t.Error("should return false for nil list")
		}
	})
}

// ---------------------------------------------------------------------------
// loadCatalogFromConfig
// ---------------------------------------------------------------------------

func TestLoadCatalogFromConfig(t *testing.T) {
	t.Run("loads catalog entries", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(map[string]any{
			"mcp_catalog.0.name":        "server-1",
			"mcp_catalog.0.description": "first server",
			"mcp_catalog.0.url":         "http://srv1",
			"mcp_catalog.1.name":        "server-2",
			"mcp_catalog.1.description": "second server",
			"mcp_catalog.1.url":         "http://srv2",
		})
		entries, err := loadCatalogFromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		if entries[0].Name != "server-1" || entries[1].Name != "server-2" {
			t.Errorf("unexpected entries: %+v", entries)
		}
	})

	t.Run("empty config returns nil", func(t *testing.T) {
		cfg := config.LoadConfigFromMap(nil)
		entries, err := loadCatalogFromConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entries != nil {
			t.Errorf("expected nil, got %d entries", len(entries))
		}
	})
}
