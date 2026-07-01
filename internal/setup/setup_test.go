package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/h2non/gock"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/limit"
	"dolphin/internal/llm"
	"dolphin/internal/llm/models"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
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

// configMapFull implements GetString, GetInt, GetFloat, GetDuration for parseProviderModels.
type configMapFull struct {
	configMap
	ints      map[string]int
	floats    map[string]float64
	durations map[string]time.Duration
	bools     map[string]bool
	headers   map[string]map[string]string
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

func (m configMapFull) GetDuration(key string) time.Duration {
	if m.durations == nil {
		return 0
	}
	return m.durations[key]
}

func (m configMapFull) GetBool(key string) bool {
	if m.bools == nil {
		return false
	}
	return m.bools[key]
}

func (m configMapFull) GetStringMap(key string) map[string]string {
	if m.headers == nil {
		return nil
	}
	return m.headers[key]
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
		// Call the shutdown to cover the closure body.
		c.OtelShutdown()
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

func TestTransportsBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()
	t.Run("stdio", func(t *testing.T) {
		mgr := session.NewManager(t.TempDir())
		c := &Context{
			Config:     config.LoadConfigFromMap(map[string]any{"stdio": map[string]any{"enabled": true}}),
			AgentIO:    agentio.NewAgentIO(0, mgr, nil, zap.NewNop(), "test"),
			SessionMgr: mgr,
			ToolReg:    tool.NewRegistry(),
			Logger:     zap.NewNop(),
		}
		b := &TransportsBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.Transports) != 1 {
			t.Errorf("expected 1 transport, got %d", len(c.Transports))
		}
	})
	t.Run("noop when already bootstrapped", func(t *testing.T) {
		c := &Context{Transports: []transport.IO{}}
		b := &TransportsBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadTransportConfigs_TUIConfig(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"tui.enabled":            true,
		"tui.show_tools":         true,
		"tui.show_thinking":      true,
		"agent.workmode":         "yolo",
		"agent.pool_size":        4,
		"agent.tool_parallelism": 2,
	})
	tcs, err := loadTransportConfigs(cfg, "test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "tui" {
			found = true
			if v, ok := tc.Config["show_tools"].(bool); !ok || !v {
				t.Error("show_tools should be true")
			}
			if v, ok := tc.Config["show_thinking"].(bool); !ok || !v {
				t.Error("show_thinking should be true")
			}
			if v, ok := tc.Config["workmode"].(string); !ok || v != "yolo" {
				t.Errorf("workmode = %q, want yolo", v)
			}
			if v, ok := tc.Config["pool_size"].(int); !ok || v != 4 {
				t.Errorf("pool_size = %d, want 4", v)
			}
			if v, ok := tc.Config["tool_parallelism"].(int); !ok || v != 2 {
				t.Errorf("tool_parallelism = %d, want 2", v)
			}
		}
	}
	if !found {
		t.Error("tui transport not found in configs")
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
		cfg := configMap{"llm.use": "gpt-4"}
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

	t.Run("empty config", func(t *testing.T) {
		cfg := configMap{}
		names := discoverProviderNames(cfg)
		if names != nil {
			t.Errorf("expected nil for empty config, got %v", names)
		}
	})
}

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
			if models[0].TopP != 0 {
				t.Errorf("TopP should default to 0, got %f", models[0].TopP)
			}
		}
	})

	t.Run("parses top_p from config", func(t *testing.T) {
		cfg := configMapFull{
			configMap: configMap{
				"llm.openai.provider":      "openai",
				"llm.openai.models.0.name": "gpt-4o",
			},
			floats: map[string]float64{"llm.openai.models.0.top_p": 0.85},
		}
		models := parseProviderModels(cfg, "openai")
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].TopP != 0.85 {
			t.Errorf("TopP = %f, want 0.85", models[0].TopP)
		}
	})

	t.Run("returns empty when no models configured", func(t *testing.T) {
		cfg := configMapFull{configMap: configMap{}}
		models := parseProviderModels(cfg, "openai")
		if len(models) != 0 {
			t.Errorf("expected 0 models, got %d", len(models))
		}
	})

	t.Run("parses per-model headers", func(t *testing.T) {
		cfg := configMapFull{
			configMap: configMap{
				"llm.openai.provider":      "openai",
				"llm.openai.models.0.name": "gpt-4o",
			},
			headers: map[string]map[string]string{
				"llm.openai.models.0.headers": {
					"X-Model-Version": "gpt-4o-2024",
					"X-Route":         "experimental",
				},
			},
		}
		models := parseProviderModels(cfg, "openai")
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if got := models[0].Headers["X-Model-Version"]; got != "gpt-4o-2024" {
			t.Errorf("X-Model-Version = %q, want %q", got, "gpt-4o-2024")
		}
		if got := models[0].Headers["X-Route"]; got != "experimental" {
			t.Errorf("X-Route = %q, want %q", got, "experimental")
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

// ---------------------------------------------------------------------------
// loadTransportConfigs
// ---------------------------------------------------------------------------

func TestLoadTransportConfigs_empty(t *testing.T) {
	cfg := config.LoadConfigFromMap(nil)
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tcs) == 0 {
		t.Fatal("expected at least stdio transport")
	}
	if tcs[0].Type != "stdio" {
		t.Errorf("expected first transport to be stdio, got %s", tcs[0].Type)
	}
}

func TestLoadTransportConfigs_explicit(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"transport.0.type":    "stdio",
		"transport.0.enabled": true,
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tcs) != 1 {
		t.Fatalf("expected 1 transport, got %d", len(tcs))
	}
	if tcs[0].Type != "stdio" {
		t.Errorf("expected stdio, got %s", tcs[0].Type)
	}
}

func TestLoadTransportConfigs_explicitDisabled(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"transport.0.type":    "stdio",
		"transport.0.enabled": false,
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tcs) != 1 {
		t.Fatalf("expected 1 fallback transport, got %d", len(tcs))
	}
	if tcs[0].Type != "stdio" {
		t.Errorf("expected fallback stdio, got %s", tcs[0].Type)
	}
}

func TestLoadTransportConfigs_a2a(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"a2a.enabled":     true,
		"a2a.addr":        ":8100",
		"a2a.name":        "test-agent",
		"a2a.description": "A test agent",
		"a2a.url":         "http://localhost:8100",
		"a2a.version":     "1.0.0",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "a2a" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a2a transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_a2a_disabled(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"a2a.enabled": false,
		"a2a.addr":    ":8100",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range tcs {
		if tc.Type == "a2a" {
			t.Errorf("expected a2a transport to be skipped when disabled")
		}
	}
}

func TestLoadTransportConfigs_a2a_skipsWithoutAddr(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"a2a.enabled": true,
		"a2a.addr":    "",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range tcs {
		if tc.Type == "a2a" {
			t.Errorf("expected a2a transport to be skipped without addr")
		}
	}
}

func TestLoadTransportConfigs_dingtalk(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"dingtalk.enabled":       true,
		"dingtalk.client_id":     "test-id",
		"dingtalk.client_secret": "test-secret",
		"dingtalk.webhook_url":   "http://webhook",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "dingtalk" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dingtalk transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_email(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"email.enabled":  true,
		"email.address":  "bot@test.com",
		"email.password": "secret",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "email" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected email transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_wework(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"wework.enabled":    true,
		"wework.bot_id":     "test-bot",
		"wework.bot_secret": "test-secret",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "wework" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected wework transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_panda(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"panda.enabled":  true,
		"panda.server":   "http://localhost:8080",
		"panda.account":  "bot",
		"panda.password": "secret",
		"panda.conv_id":  "conv1",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "panda" {
			found = true
			if tc.Config["conv_id"] != "conv1" {
				t.Errorf("expected conv_id=conv1, got %v", tc.Config["conv_id"])
			}
			if v, ok := tc.Config["allow_convs"].(string); ok && v != "" {
				t.Errorf("expected empty allow_convs when not set, got %q", v)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected panda transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_panda_allowConvs(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"panda.enabled":     true,
		"panda.server":      "http://localhost:8080",
		"panda.account":     "bot",
		"panda.password":    "secret",
		"panda.allow_convs": "conv1,conv2",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "panda" {
			found = true
			ac, ok := tc.Config["allow_convs"].(string)
			if !ok || ac != "conv1,conv2" {
				t.Errorf("expected allow_convs='conv1,conv2', got %v", tc.Config["allow_convs"])
			}
			break
		}
	}
	if !found {
		t.Errorf("expected panda transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_weworkEnvFallback(t *testing.T) {
	// When bot_id/bot_secret are not in config, fall back to env vars.
	t.Setenv("WEWORK", "env-bot-id")
	t.Setenv("WESecret", "env-bot-secret")
	cfg := config.LoadConfigFromMap(map[string]any{
		"wework.enabled": true,
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, tc := range tcs {
		if tc.Type == "wework" {
			found = true
			if tc.Config["bot_id"] != "env-bot-id" {
				t.Errorf("expected bot_id='env-bot-id', got %v", tc.Config["bot_id"])
			}
			if tc.Config["bot_secret"] != "env-bot-secret" {
				t.Errorf("expected bot_secret='env-bot-secret', got %v", tc.Config["bot_secret"])
			}
			break
		}
	}
	if !found {
		t.Errorf("expected wework transport, got %v", tcs)
	}
}

func TestLoadTransportConfigs_skipsEmailWithoutPassword(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"email.enabled": true,
		"email.address": "bot@test.com",
	})
	tcs, err := loadTransportConfigs(cfg, "dolphin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tc := range tcs {
		if tc.Type == "email" {
			t.Fatal("email should not be added without password")
		}
	}
}

// ---------------------------------------------------------------------------
// loadMCPServers
// ---------------------------------------------------------------------------

func TestLoadMCPServers_empty(t *testing.T) {
	cfg := config.LoadConfigFromMap(nil)
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_unknownBuiltin(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "nonexistent",
		"mcp_servers.0.type":    "builtin",
		"mcp_servers.0.enabled": true,
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_missingURL(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "srv",
		"mcp_servers.0.type":    "url",
		"mcp_servers.0.enabled": true,
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_missingCommand(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "srv",
		"mcp_servers.0.type":    "stdio",
		"mcp_servers.0.enabled": true,
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_disabled(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "srv",
		"mcp_servers.0.type":    "url",
		"mcp_servers.0.enabled": false,
		"mcp_servers.0.url":     "http://example.com",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_urlType(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "remote-srv",
		"mcp_servers.0.type":    "url",
		"mcp_servers.0.enabled": true,
		"mcp_servers.0.url":     "http://example.com/mcp",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_httpType(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "http-srv",
		"mcp_servers.0.type":    "http",
		"mcp_servers.0.enabled": true,
		"mcp_servers.0.url":     "http://example.com/http-mcp",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_multipleServers(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "disabled-srv",
		"mcp_servers.0.type":    "url",
		"mcp_servers.0.enabled": false,
		"mcp_servers.0.url":     "http://example.com/disabled",
		"mcp_servers.1.name":    "active-srv",
		"mcp_servers.1.type":    "url",
		"mcp_servers.1.enabled": true,
		"mcp_servers.1.url":     "http://example.com/active",
		"mcp_servers.2.type":    "stdio",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_stdioWithArgs(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "cli-srv",
		"mcp_servers.0.type":    "stdio",
		"mcp_servers.0.enabled": true,
		"mcp_servers.0.command": "nonexistent-cmd",
		"mcp_servers.0.args.0":  "--flag",
		"mcp_servers.0.args.1":  "value",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_builtinRegistration(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "playwright",
		"mcp_servers.0.type":    "builtin",
		"mcp_servers.0.enabled": true,
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

func TestLoadMCPServers_iterationBreak(t *testing.T) {
	cfg := config.LoadConfigFromMap(map[string]any{
		"mcp_servers.0.name":    "srv-0",
		"mcp_servers.0.type":    "url",
		"mcp_servers.0.enabled": true,
		"mcp_servers.0.url":     "http://example.com/0",
		"mcp_servers.2.name":    "srv-2",
		"mcp_servers.2.type":    "url",
		"mcp_servers.2.enabled": true,
		"mcp_servers.2.url":     "http://example.com/2",
	})
	reg := tool.NewRegistry()
	logger, _ := zap.NewDevelopment()

	loadMCPServers(cfg, reg, logger)
}

// ---------------------------------------------------------------------------
// LLMBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestLLMBootstrapperBootstrap_legacy(t *testing.T) {
	b := &LLMBootstrapper{}
	logC := &Context{Config: config.LoadConfigFromMap(map[string]any{
		"log":     map[string]any{"level": "debug"},
		"llm.use": "gpt-4",
	})}
	logB := &LoggerBootstrapper{}
	if err := logB.Bootstrap(context.Background(), logC); err != nil {
		t.Fatal(err)
	}

	c := &Context{Config: config.LoadConfigFromMap(map[string]any{
		"llm.use": "gpt-4",
	}), Logger: logC.Logger}

	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.LLMProvider == nil {
		t.Fatal("LLMProvider should be set")
	}
}

func TestLLMBootstrapperBootstrap_noop(t *testing.T) {
	b := &LLMBootstrapper{}
	c := &Context{LLMProvider: &llmProviderMock{}}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type llmProviderMock struct{}

func (m *llmProviderMock) Name() string { return "mock" }
func (m *llmProviderMock) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk)
	close(ch)
	return ch, nil
}

func (m *llmProviderMock) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// SchedulerBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestSchedulerBootstrapperBootstrap_noop(t *testing.T) {
	b := &SchedulerBootstrapper{}
	c := &Context{Scheduler: &scheduler.Scheduler{}}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AgentIOBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestAgentIOBootstrapperBootstrap_noop(t *testing.T) {
	b := &AgentIOBootstrapper{}
	c := &Context{AgentIO: &agentio.AgentIO{}}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ToolsBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestToolsBootstrapperBootstrap_noop(t *testing.T) {
	b := &ToolsBootstrapper{}
	c := &Context{ToolReg: tool.NewRegistry()}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TransportsBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestTransportsBootstrapperBootstrap_noop(t *testing.T) {
	b := &TransportsBootstrapper{}
	c := &Context{Transports: make([]transport.IO, 0)}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTransportsBootstrapperBootstrap_stdioFallback(t *testing.T) {
	b := &TransportsBootstrapper{}
	cfg := config.LoadConfigFromMap(map[string]any{"agent.name": "dolphin"})
	logger := zap.NewNop()
	mgr := session.NewManager(t.TempDir())
	bus := signal.NewBus()
	io := agentio.NewAgentIO(100, mgr, bus, logger, "dolphin")
	c := &Context{
		Config:     cfg,
		Logger:     logger,
		SessionMgr: mgr,
		AgentIO:    io,
		ToolReg:    tool.NewRegistry(),
	}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Transports) == 0 {
		t.Fatal("expected at least one transport (stdio fallback)")
	}
}

// ---------------------------------------------------------------------------
// SchedulerBootstrapper Bootstrap
// ---------------------------------------------------------------------------

func TestSchedulerBootstrapperBootstrap_full(t *testing.T) {
	b := &SchedulerBootstrapper{}
	dir := t.TempDir()
	c := &Context{
		Config: config.LoadConfigFromMap(map[string]any{
			"memory": map[string]any{"dir": dir},
		}),
		Logger:  zap.NewNop(),
		Brain:   &brain.Brain{},
		ToolReg: tool.NewRegistry(),
		CmdReg:  command.NewRegistry(nil, nil),
	}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Scheduler == nil {
		t.Fatal("Scheduler should be set")
	}
}

func TestBrainBootstrapperBootstrap_noop(t *testing.T) {
	b := &BrainBootstrapper{}
	c := &Context{Brain: &brain.Brain{}}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBrainBootstrapperBootstrap_full(t *testing.T) {
	b := &BrainBootstrapper{}
	dir := t.TempDir()

	buses := &BusesBootstrapper{}
	mgr := session.NewManager(dir)
	sb := signal.NewBus()
	c := &Context{
		Config: config.LoadConfigFromMap(map[string]any{
			"brain.dir": dir,
		}),
		Logger:  zap.NewNop(),
		ToolReg: tool.NewRegistry(),
		CmdReg:  command.NewRegistry(mgr, sb),
	}
	err := buses.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("buses bootstrap: %v", err)
	}

	err = b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Brain == nil {
		t.Fatal("Brain should be set")
	}
}

// ---------------------------------------------------------------------------
// bootstrapSection
// ---------------------------------------------------------------------------

func TestBootstrapSection_registersRegisteredModel(t *testing.T) {
	// Register a throwaway per-model provider for the test model.
	llm.RegisterModelProvider("dolphin-test-model/openai", models.NewOpenAIProvider("dolphin-test-model"))
	defer llm.UnregisterModelProvider("dolphin-test-model/openai")

	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.test.api_key":             "sk-test",
		"llm.test.provider":            "test",
		"llm.test.api_type":            "openai",
		"llm.test.base_url":            "http://test",
		"llm.test.models.0.name":       "dolphin-test-model",
		"llm.test.models.0.max_tokens": 100,
	})
	c := &Context{Config: cfg, Logger: zap.NewNop()}
	mgr := llm.NewManager()
	c.bootstrapSection(context.Background(), "test", mgr)

	models, err := mgr.Models(context.Background())
	if err != nil {
		t.Fatalf("Models error: %v", err)
	}
	found := false
	for _, m := range models {
		if m.Name == "dolphin-test-model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dolphin-test-model to be registered, got %v", models)
	}
}

func TestBootstrapSection_fallsBackToGenericShell(t *testing.T) {
	// No provider registered for "no-such-model/openai" — falls back to the
	// generic OpenAI shell provider instead of skipping the model.
	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.test.api_key":       "sk-test",
		"llm.test.api_type":      "openai",
		"llm.test.base_url":      "http://test",
		"llm.test.models.0.name": "no-such-model",
	})
	c := &Context{Config: cfg, Logger: zap.NewNop()}
	mgr := llm.NewManager()
	c.bootstrapSection(context.Background(), "test", mgr)

	models, _ := mgr.Models(context.Background())
	found := false
	for _, m := range models {
		if m.Name == "no-such-model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("model should have been registered via fallback, got %v", models)
	}
}

func TestBootstrapSection_modelDiscoverWiresRegisteredModels(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "deepseek-v4-flash"}, {"id": "unregistered"}}})

	llm.RegisterModelProvider("deepseek-v4-flash/openai", models.NewOpenAIProvider("deepseek-v4-flash"))
	defer llm.UnregisterModelProvider("deepseek-v4-flash/openai")

	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.test.api_key":        "sk-test",
		"llm.test.api_type":       "openai",
		"llm.test.base_url":       "https://api.openai.com",
		"llm.test.model_discover": true,
	})
	c := &Context{Config: cfg, Logger: zap.NewNop()}
	mgr := llm.NewManager()
	c.bootstrapSection(context.Background(), "test", mgr)

	models, _ := mgr.Models(context.Background())
	hasRegistered := false
	hasUnregistered := false
	for _, m := range models {
		if m.Name == "deepseek-v4-flash" {
			hasRegistered = true
		}
		if m.Name == "unregistered" {
			hasUnregistered = true
		}
	}
	if !hasRegistered {
		t.Fatalf("expected deepseek-v4-flash to be wired, got %v", models)
	}
	if !hasUnregistered {
		t.Fatalf("discovered model without per-model provider should fall back to generic shell, got %v", models)
	}
}

func TestBootstrapSection_modelDiscoverErrorIsNonFatal(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").Get("/v1/models").Reply(401)

	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.test.api_key":        "bad-key",
		"llm.test.api_type":       "openai",
		"llm.test.base_url":       "https://api.openai.com",
		"llm.test.model_discover": true,
	})
	c := &Context{Config: cfg, Logger: zap.NewNop()}
	mgr := llm.NewManager()
	// Must not panic; section just yields no models.
	c.bootstrapSection(context.Background(), "test", mgr)
}

// ---------------------------------------------------------------------------
// llm.DiscoverModels dispatch
// ---------------------------------------------------------------------------

func TestDiscoverModels_openai(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.openai.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "gpt-4"}}})

	cfg := llm.Config{APIType: "openai", APIKey: "sk-test", BaseURL: "https://api.openai.com"}
	models, err := llm.DiscoverModels(context.Background(), cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "gpt-4" {
		t.Fatalf("unexpected: %+v", models)
	}
}

func TestDiscoverModels_anthropic(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.anthropic.com").
		Get("/v1/models").
		Reply(200).
		JSON(map[string]any{"data": []map[string]any{{"id": "claude-3"}}})

	cfg := llm.Config{APIType: "anthropic", APIKey: "ant-key", BaseURL: "https://api.anthropic.com"}
	models, err := llm.DiscoverModels(context.Background(), cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "claude-3" {
		t.Fatalf("unexpected: %+v", models)
	}
}

func TestAgentIOBootstrapperBootstrap_full(t *testing.T) {
	b := &AgentIOBootstrapper{}
	dir := t.TempDir()
	sessMgr := session.NewManager(dir)
	sigBus := signal.NewBus()
	evBus := event.NewBus()
	toolReg := tool.NewRegistry()
	cmdReg := command.NewRegistry(sessMgr, sigBus)
	cfg := config.LoadConfigFromMap(map[string]any{
		"agent": map[string]any{
			"buffer_size":  0,
			"max_rounds":   0,
			"turn_timeout": "30s",
			"name":         "test-agent",
			"workmode":     "yolo",
			"workspace":    dir,
		},
		"permission": map[string]any{
			"file": "",
		},
		"llm": map[string]any{
			"max_tokens":  4096,
			"max_retries": 3,
		},
		"tool": map[string]any{
			"timeout": "10s",
		},
	})
	c := &Context{
		Config:      cfg,
		Logger:      zap.NewNop(),
		SessionMgr:  sessMgr,
		SignalBus:   sigBus,
		EventBus:    evBus,
		ToolReg:     toolReg,
		CmdReg:      cmdReg,
		LLMProvider: &llmProviderMock{},
		Mem:         nil, // MemoryReadStage and MemoryWriteStage will just have nil Memory
		SkillStore:  nil,
		Brain:       &brain.Brain{},
		HookReg:     nil,
	}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AgentIO == nil {
		t.Fatal("AgentIO should be set")
	}
	if c.AgentLoop == nil {
		t.Fatal("AgentLoop should be set")
	}
}

func TestPprofBootstrapper(t *testing.T) {
	b := &PprofBootstrapper{}
	if b.Name() != "pprof" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 111 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestPprofBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("no-op when PprofShutdown already set", func(t *testing.T) {
		c := &Context{PprofShutdown: func() {}}
		b := &PprofBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no-op when pprof.enabled is false", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(map[string]any{
			"pprof.enabled": false,
		}), Logger: zap.NewNop()}
		b := &PprofBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.PprofShutdown != nil {
			t.Error("PprofShutdown should remain nil when disabled")
		}
	})

	t.Run("no-op when pprof.enabled is missing", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(nil), Logger: zap.NewNop()}
		b := &PprofBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.PprofShutdown != nil {
			t.Error("PprofShutdown should remain nil when not enabled")
		}
	})

	t.Run("starts pprof server when enabled", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(map[string]any{
			"pprof.enabled": true,
			"pprof.addr":    "127.0.0.1:0", // port 0 = random available port
		}), Logger: zap.NewNop()}
		b := &PprofBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.PprofShutdown == nil {
			t.Fatal("PprofShutdown should be set when enabled")
		}
		c.PprofShutdown()
	})

	t.Run("uses default addr when pprof.addr is empty", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(map[string]any{
			"pprof.enabled": true,
		}), Logger: zap.NewNop()}
		b := &PprofBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.PprofShutdown == nil {
			t.Fatal("PprofShutdown should be set with default addr")
		}
		c.PprofShutdown()
	})
}

func TestToolsBootstrapperBootstrap_full(t *testing.T) {
	b := &ToolsBootstrapper{}
	dir := t.TempDir()
	sessMgr := session.NewManager(dir)
	sigBus := signal.NewBus()
	cfg := config.LoadConfigFromMap(map[string]any{
		"brain.dir": dir,
	})
	c := &Context{
		Config:      cfg,
		Logger:      zap.NewNop(),
		SessionMgr:  sessMgr,
		SignalBus:   sigBus,
		LLMProvider: &llmProviderMock{},
		// ToolReg is nil — this forces the full Bootstrap path
	}
	err := b.Bootstrap(context.Background(), c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ToolReg == nil {
		t.Fatal("ToolReg should be set")
	}
	if c.CmdReg == nil {
		t.Fatal("CmdReg should be set")
	}
	if c.CmdReg != nil && !c.CmdReg.HasCommand("queue") {
		t.Fatal("queue command should be registered")
	}
}

// ---------------------------------------------------------------------------
// CLIBootstrapper
// ---------------------------------------------------------------------------

func TestCLIBootstrapper(t *testing.T) {
	b := &CLIBootstrapper{}
	if b.Name() != "cli" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Index() != 85 {
		t.Errorf("Index() = %d", b.Index())
	}
}

func TestCLIBootstrapperBootstrap(t *testing.T) {
	ctx := context.Background()

	t.Run("no-op when agent.bin not configured", func(t *testing.T) {
		c := &Context{Config: config.LoadConfigFromMap(nil)}
		b := &CLIBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.ContextSections) != 0 {
			t.Error("ContextSections should be empty")
		}
	})

	t.Run("no-op when agent.bin dirs have no executables", func(t *testing.T) {
		dir := t.TempDir()
		c := &Context{
			Config: config.LoadConfigFromMap(map[string]any{
				"agent.bin": dir,
			}),
			Logger: zap.NewNop(),
		}
		b := &CLIBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.ContextSections) != 0 {
			t.Error("ContextSections should be empty when no executables found")
		}
	})

	t.Run("registers CLI section and re-registers shell when executables found", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping on windows")
		}
		dir := t.TempDir()
		script := filepath.Join(dir, "demo")
		_ = os.WriteFile(script, []byte("#!/bin/sh\necho Usage: demo '<args>'"), 0o755)

		c := &Context{
			Config: config.LoadConfigFromMap(map[string]any{
				"agent.bin": dir,
			}),
			Logger:  zap.NewNop(),
			ToolReg: tool.NewRegistry(),
		}
		b := &CLIBootstrapper{}
		err := b.Bootstrap(ctx, c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.ContextSections) != 1 {
			t.Fatalf("expected 1 context section, got %d", len(c.ContextSections))
		}
	})
}

func TestWorkflowBootstrapper(t *testing.T) {
	Convey("WorkflowBootstrapper", t, func() {
		b := &WorkflowBootstrapper{}
		Convey("Name returns workflow", func() {
			So(b.Name(), ShouldEqual, "workflow")
		})
		Convey("Index returns 91", func() {
			So(b.Index(), ShouldEqual, 91)
		})
		Convey("Bootstrap sets WorkflowEngine", func() {
			logger, _ := zap.NewDevelopment()
			cfg := config.LoadConfigFromMap(map[string]any{"llm.use": "openai", "llm.openai.api_key": "sk-test"})
			provider := models.NewProvider(llm.Config{
				Provider: "openai",
				Model:    "gpt-4o",
				APIKey:   "sk-test",
				BaseURL:  "http://127.0.0.1:1",
			}, logger)
			c := &Context{
				ToolReg:     tool.NewRegistry(),
				LLMProvider: provider,
				EventBus:    event.NewBus(),
				Logger:      logger,
				AgentIO:     agentio.NewAgentIO(10, session.NewManager(t.TempDir()), signal.NewBus(), logger, "test"),
				Config:      cfg,
			}
			err := b.Bootstrap(context.Background(), c)
			So(err, ShouldBeNil)
			So(c.WorkflowEngine, ShouldNotBeNil)
		})
	})
}

// stubProvider is a minimal llm.Provider for lookupTemperature tests,
// returning a fixed model list (or an error).
type stubProvider struct {
	name   string
	models []llm.ModelConfig
	err    error
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubProvider) Models(ctx context.Context) ([]llm.ModelConfig, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.models, nil
}

func TestLookupTemperature(t *testing.T) {
	Convey("lookupTemperature", t, func() {
		Convey("nil provider or empty model returns 0", func() {
			So(lookupTemperature(nil, "gpt"), ShouldEqual, 0)
			So(lookupTemperature(&stubProvider{}, ""), ShouldEqual, 0)
		})

		Convey("provider error returns 0", func() {
			p := &stubProvider{err: fmt.Errorf("boom")}
			So(lookupTemperature(p, "gpt"), ShouldEqual, 0)
		})

		Convey("matches by exact name", func() {
			p := &stubProvider{models: []llm.ModelConfig{
				{Name: "gpt-4o", Temperature: 0.7},
				{Name: "claude", Temperature: 0.5},
			}}
			So(lookupTemperature(p, "gpt-4o"), ShouldEqual, 0.7)
		})

		Convey("matches by short name after provider/ prefix", func() {
			p := &stubProvider{models: []llm.ModelConfig{
				{Name: "sonnet", Temperature: 0.9},
			}}
			// "anthropic/sonnet" should match the short "sonnet".
			So(lookupTemperature(p, "anthropic/sonnet"), ShouldEqual, 0.9)
		})

		Convey("unknown model returns 0", func() {
			p := &stubProvider{models: []llm.ModelConfig{
				{Name: "gpt-4o", Temperature: 0.7},
			}}
			So(lookupTemperature(p, "nope"), ShouldEqual, 0)
		})
	})
}

func TestConfigHasKey(t *testing.T) {
	Convey("configHasKey", t, func() {
		Convey("true when key present", func() {
			cfg := config.LoadConfigFromMap(map[string]any{
				"agent.workmode": "yolo",
			})
			So(configHasKey(cfg, "agent.workmode"), ShouldBeTrue)
		})
		Convey("false when key absent", func() {
			cfg := config.LoadConfigFromMap(nil)
			So(configHasKey(cfg, "agent.workmode"), ShouldBeFalse)
		})
	})
}
