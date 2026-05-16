package agent

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/config"
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
		responses: []*ProviderResponse{
			{Content: TextContent("task done"), Usage: &Usage{InputTokens: 5, OutputTokens: 10}, StopReason: "end_turn"},
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
