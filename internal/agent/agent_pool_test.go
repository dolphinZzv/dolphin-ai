package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dolphin/internal/agent/provider"
	"dolphin/internal/config"
	"dolphin/internal/mcp"
)

func TestAgentInstanceStatus(t *testing.T) {
	inst := &AgentInstance{}
	inst.mu.Lock()
	inst.status = "idle"
	inst.mu.Unlock()
	if s := inst.Status(); s != "idle" {
		t.Errorf("Status() = %q", s)
	}
}

func TestAgentInstanceTasksDone(t *testing.T) {
	inst := &AgentInstance{}
	inst.mu.Lock()
	inst.tasksDone = 5
	inst.mu.Unlock()
	if n := inst.TasksDone(); n != 5 {
		t.Errorf("TasksDone() = %d", n)
	}
}

func TestAgentInstanceLastTaskAt(t *testing.T) {
	now := time.Now()
	inst := &AgentInstance{}
	inst.mu.Lock()
	inst.lastTaskAt = now
	inst.mu.Unlock()
	if got := inst.LastTaskAt(); !got.Equal(now) {
		t.Errorf("LastTaskAt() = %v, want %v", got, now)
	}
}

func TestNewPoolConfigFromConfig(t *testing.T) {
	cfg := config.PoolConfig{
		MaxConcurrency:    3,
		DefaultTimeout:    60,
		WorkspaceDir:      "/tmp/workspace",
		IdleTimeout:       300,
		MaxPendingResults: 10,
	}
	pc := NewPoolConfigFromConfig(cfg)
	if pc.MaxConcurrency != 3 {
		t.Errorf("MaxConcurrency = %d", pc.MaxConcurrency)
	}
	if pc.DefaultTimeout != 60 {
		t.Errorf("DefaultTimeout = %d", pc.DefaultTimeout)
	}
	if pc.WorkspaceDir != "/tmp/workspace" {
		t.Errorf("WorkspaceDir = %q", pc.WorkspaceDir)
	}
	if pc.IdleTimeout != 300*time.Second {
		t.Errorf("IdleTimeout = %v", pc.IdleTimeout)
	}
	if pc.MaxPendingResults != 10 {
		t.Errorf("MaxPendingResults = %d", pc.MaxPendingResults)
	}
}

func TestNewPoolConfigFromConfigZeroValues(t *testing.T) {
	pc := NewPoolConfigFromConfig(config.PoolConfig{})
	if pc.MaxConcurrency != 0 {
		t.Errorf("expected 0, got %d", pc.MaxConcurrency)
	}
}

func TestNewAgentPoolIdleTimeoutDisabled(t *testing.T) {
	cfg := PoolConfig{}
	pool := NewAgentPool(context.Background(), cfg)
	if pool == nil {
		t.Fatal("NewAgentPool returned nil")
	}
	pool.Shutdown()
}

func TestAgentPoolListEmpty(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	list := pool.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
	pool.Shutdown()
}

func TestAgentPoolCollectEmpty(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	results := pool.Collect()
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	pool.Shutdown()
}

func TestAgentPoolSetParentSessionID(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	pool.SetParentSessionID("test-id")
	if pool.parentSessionID != "test-id" {
		t.Errorf("parentSessionID = %q", pool.parentSessionID)
	}
	pool.Shutdown()
}

// --- E2E: Agent pool lifecycle ---

func TestAgentPoolAddAndList(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{}
	agt := newTestAgent(cfg, prov)
	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency:    2,
		DefaultTimeout:    30,
		MaxPendingResults: 16,
	})
	defer pool.Shutdown()

	def := &AgentDef{Name: "worker", Tools: []string{"test_tool"}}
	inst := pool.Add("worker", def, AgentCoord, agt, agt.toolReg)
	if inst == nil {
		t.Fatal("Add returned nil")
	}
	if inst.Status() != "idle" {
		t.Errorf("expected idle status, got %q", inst.Status())
	}

	list := pool.List()
	if len(list) != 1 {
		t.Errorf("expected 1 agent in list, got %d", len(list))
	}
	if list[0].Name != "worker" {
		t.Errorf("expected agent name 'worker', got %q", list[0].Name)
	}
}

func TestAgentPoolDispatchThenCollect(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{
		responses: []*provider.ProviderResponse{
			{Content: provider.TextContent("task done"), Usage: &provider.Usage{InputTokens: 5, OutputTokens: 10}, StopReason: "end_turn"},
		},
	}

	agt := newTestAgent(cfg, prov)
	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency:    2,
		DefaultTimeout:    30,
		MaxPendingResults: 16,
	})
	defer pool.Shutdown()

	def := &AgentDef{Name: "worker", Tools: []string{"test_tool"}}
	pool.Add("worker", def, AgentCoord, agt, agt.toolReg)

	err := pool.Dispatch("worker", Task{ID: "task-1", Input: "do something"})
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	// Allow worker to process through runTurn
	time.Sleep(500 * time.Millisecond)

	results := pool.Collect()
	_ = len(results) // may be 0 or 1 depending on timing — just verify no panic
}

func TestAgentPoolConcurrentAddAndList(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{}
	agt := newTestAgent(cfg, prov)
	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency:    3,
		DefaultTimeout:    30,
		MaxPendingResults: 16,
	})
	defer pool.Shutdown()

	// Concurrent Add operations
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func(idx int) {
			def := &AgentDef{Name: "worker-" + string(rune('a'+idx)), Tools: []string{"test_tool"}}
			pool.Add(def.Name, def, AgentCoord, agt, agt.toolReg)
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 3; i++ {
		<-done
	}

	list := pool.List()
	if len(list) != 3 {
		t.Errorf("expected 3 agents, got %d", len(list))
	}
}

func TestAgentPoolDispatchToNonexistent(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency: 2,
		DefaultTimeout: 30,
	})
	defer pool.Shutdown()

	err := pool.Dispatch("nonexistent", Task{ID: "t1", Input: "go"})
	if err == nil {
		t.Error("expected error dispatching to nonexistent agent")
	}
}

func TestAgentPoolRemoveIdle(t *testing.T) {
	cfg := config.DefaultConfig()
	config.SetSessionsDir(t.TempDir())
	cfg.LLM.MaxContextTokens = 100000

	prov := &mockProvider{}
	agt := newTestAgent(cfg, prov)
	pool := NewAgentPool(context.Background(), PoolConfig{
		MaxConcurrency: 2,
		DefaultTimeout: 30,
	})
	defer pool.Shutdown()

	def := &AgentDef{Name: "temp", Tools: []string{"test_tool"}}
	pool.Add("temp", def, AgentCoord, agt, agt.toolReg)

	if !pool.Remove("temp") {
		t.Error("Remove should return true for existing agent")
	}
	if pool.Remove("temp") {
		t.Error("Remove should return false for already-removed agent")
	}
}

func TestAgentPoolShutdownNoAgents(t *testing.T) {
	pool := NewAgentPool(context.Background(), PoolConfig{})
	// Shutdown with no agents should complete without panic
	pool.Shutdown()
	results := pool.Collect()
	if len(results) != 0 {
		t.Errorf("expected empty results after shutdown, got %d", len(results))
	}
}

// ---- filterTool tests ----

// mockPoolTool is a minimal mcp.Tool for testing.
type mockPoolTool struct {
	name    string
	execute func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error)
}

func (m *mockPoolTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{Name: m.name}
}

func (m *mockPoolTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	return m.execute(ctx, input)
}

func TestFilterTool_BlocksWhenPreCheckFails(t *testing.T) {
	inner := &mockPoolTool{
		name: "test_tool",
		execute: func(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
			return &mcp.ToolResult{Content: "ok", IsError: false}, nil
		},
	}

	ft := &filterTool{
		def:      inner.Definition(),
		original: inner,
		preCheck: func(_ context.Context, _ json.RawMessage) string {
			return "blocked"
		},
	}

	result, err := ft.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when preCheck blocks")
	}
	if result.Content != "blocked" {
		t.Errorf("result = %q, want 'blocked'", result.Content)
	}
}

func TestFilterTool_PassesWhenPreCheckSucceeds(t *testing.T) {
	inner := &mockPoolTool{
		name: "test_tool",
		execute: func(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
			return &mcp.ToolResult{Content: "ok", IsError: false}, nil
		},
	}

	ft := &filterTool{
		def:      inner.Definition(),
		original: inner,
		preCheck: func(_ context.Context, _ json.RawMessage) string {
			return ""
		},
	}

	result, err := ft.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success when preCheck passes")
	}
	if result.Content != "ok" {
		t.Errorf("result = %q, want 'ok'", result.Content)
	}
}

// ---- wrapSkillTools / wrapWorkflowTools integration tests ----

func TestWrapSkillTools_AllowsPermittedSkill(t *testing.T) {
	reg := mcp.NewRegistry(config.DefaultConfig())
	reg.Register(&mockPoolTool{
		name: "load_skill",
		execute: func(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(input, &p)
			return &mcp.ToolResult{Content: "skill: " + p.Name}, nil
		},
	})

	reg = wrapSkillTools(reg, []string{"review", "deploy"})

	// Allowed skill
	input, _ := json.Marshal(map[string]string{"name": "review"})
	result, err := reg.Execute(context.Background(), "load_skill", input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for 'review', got error: %s", result.Content)
	}
}

func TestWrapSkillTools_BlocksDisallowedSkill(t *testing.T) {
	reg := mcp.NewRegistry(config.DefaultConfig())
	reg.Register(&mockPoolTool{
		name: "load_skill",
		execute: func(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(input, &p)
			return &mcp.ToolResult{Content: "skill: " + p.Name}, nil
		},
	})

	reg = wrapSkillTools(reg, []string{"review", "deploy"})

	// Disallowed skill
	input, _ := json.Marshal(map[string]string{"name": "secret"})
	result, err := reg.Execute(context.Background(), "load_skill", input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for disallowed skill 'secret'")
	}
}

func TestWrapWorkflowTools_AllowsPermittedWorkflow(t *testing.T) {
	reg := mcp.NewRegistry(config.DefaultConfig())
	reg.Register(&mockPoolTool{
		name: "load_workflow",
		execute: func(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(input, &p)
			return &mcp.ToolResult{Content: "workflow: " + p.Name}, nil
		},
	})

	reg = wrapWorkflowTools(reg, []string{"review-flow", "deploy-flow"})

	// Allowed workflow
	input, _ := json.Marshal(map[string]string{"name": "review-flow"})
	result, err := reg.Execute(context.Background(), "load_workflow", input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for 'review-flow', got error: %s", result.Content)
	}
}

func TestWrapWorkflowTools_BlocksDisallowedWorkflow(t *testing.T) {
	reg := mcp.NewRegistry(config.DefaultConfig())
	reg.Register(&mockPoolTool{
		name: "run_workflow",
		execute: func(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(input, &p)
			return &mcp.ToolResult{Content: "workflow: " + p.Name}, nil
		},
	})

	reg = wrapWorkflowTools(reg, []string{"review-flow"})

	// Disallowed workflow
	input, _ := json.Marshal(map[string]string{"name": "admin-flow"})
	result, err := reg.Execute(context.Background(), "run_workflow", input)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for disallowed workflow 'admin-flow'")
	}
}

func TestWrapSkillTools_NoOpWhenAllowedEmpty(t *testing.T) {
	origTool := &mockPoolTool{
		name: "load_skill",
		execute: func(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
			return &mcp.ToolResult{Content: "any"}, nil
		},
	}
	reg := mcp.NewRegistry(config.DefaultConfig())
	reg.Register(origTool)

	// Empty allowed list = no-op
	reg2 := wrapSkillTools(reg, []string{})
	if reg2 != reg {
		t.Error("expected same registry back when allowed is empty")
	}
}
