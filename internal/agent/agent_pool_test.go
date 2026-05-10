package agent

import (
	"context"
	"testing"
	"time"

	"dolphinzZ/internal/config"
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
