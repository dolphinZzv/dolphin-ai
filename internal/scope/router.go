package scope

import (
	"context"
	"fmt"
	"time"
)

// ScopeRouter routes tasks to scope agents.
//
// Coordinator interacts with this interface rather than concrete
// Manager or AgentPool types, enabling pluggable routing strategies.
type ScopeRouter interface {
	// Resolve maps file paths to scope names.
	Resolve(paths []string) (map[string][]string, error)

	// Dispatch sends a task to a scope agent and waits for the result.
	Dispatch(ctx context.Context, scope string, task DispatchTask) ([]DispatchResult, error)

	// Scopes returns information about all known scopes.
	Scopes() []ScopeInfo
}

// DispatchTask is a task to be executed by a scope agent.
type DispatchTask struct {
	ID      string `json:"id"`
	Input   string `json:"input"`
	Timeout int    `json:"timeout,omitempty"`
}

// DispatchResult is the result of a dispatched task.
type DispatchResult struct {
	TaskID    string `json:"task_id"`
	AgentName string `json:"agent_name"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	Success   bool   `json:"success"`
}

// ScopeInfo holds lightweight scope metadata for display.
type ScopeInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Dirs        []string `json:"dirs"`
}

// Dispatcher is the minimal interface AgentPool must satisfy for
// scope routing. Defined here to avoid a circular dependency between
// the scope and agent packages.
type Dispatcher interface {
	// Dispatch sends a task to an agent (non-blocking, returns nil on success).
	Dispatch(agentName string, task DispatchTask) error

	// PollResult checks for a completed result by task ID (non-blocking).
	PollResult(taskID string) *DispatchResult
}

// RouterConfig controls the routing strategy.
type RouterConfig struct {
	Type string `yaml:"type"` // local | agent | mqtt
}

// NewRouter creates a ScopeRouter based on the given config.
//
// If mgr is nil, returns an emptyRouter that always returns empty results
// and a clear error message on Dispatch.
func NewRouter(cfg RouterConfig, mgr *Manager, pool Dispatcher) ScopeRouter {
	if mgr == nil {
		return &emptyRouter{}
	}
	switch cfg.Type {
	case "local":
		return &localRouter{mgr: mgr, pool: pool}
	default:
		return &localRouter{mgr: mgr, pool: pool}
	}
}

// emptyRouter is a no-op router used when no scopes are configured.
type emptyRouter struct{}

func (r *emptyRouter) Resolve(_ []string) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (r *emptyRouter) Dispatch(_ context.Context, _ string, _ DispatchTask) ([]DispatchResult, error) {
	return nil, fmt.Errorf("scope router not configured: no scopes found, check .dolphin/agents/scopes.yaml")
}

func (r *emptyRouter) Scopes() []ScopeInfo { return nil }

// defaultPollInterval is how often localRouter polls for result completion.
const defaultPollInterval = 100 * time.Millisecond

// localRouter implements ScopeRouter using Go-level directory prefix matching
// and in-process agent dispatch.
type localRouter struct {
	mgr  *Manager
	pool Dispatcher
}

func (r *localRouter) Resolve(paths []string) (map[string][]string, error) {
	return r.mgr.Resolve(paths), nil
}

func (r *localRouter) Dispatch(ctx context.Context, scope string, task DispatchTask) ([]DispatchResult, error) {
	if err := r.pool.Dispatch(scope, task); err != nil {
		return nil, fmt.Errorf("dispatch to scope %q failed: %w", scope, err)
	}

	for {
		if res := r.pool.PollResult(task.ID); res != nil {
			return []DispatchResult{*res}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(defaultPollInterval):
		}
	}
}

func (r *localRouter) Scopes() []ScopeInfo {
	if r.mgr == nil {
		return nil
	}
	return r.mgr.Info()
}
