package agent

import "dolphin/internal/metrics"

// Agent-level metrics collected via the global metrics registry.
var (
	// Task metrics
	taskDispatched = metrics.NewCounter("agent_tasks_dispatched_total", "Total tasks dispatched to sub-agents", map[string]string{})
	taskCompleted  = metrics.NewCounter("agent_tasks_completed_total", "Total tasks completed by sub-agents", map[string]string{})
	taskFailed     = metrics.NewCounter("agent_tasks_failed_total", "Total sub-agent task failures", map[string]string{})

	// Pool gauge (updated by pool lifecycle)
	agentPoolSize = metrics.NewGauge("agent_pool_size", "Current number of registered agents in pool", map[string]string{})
	activeAgents  = metrics.NewGauge("agent_active_agents", "Current number of active (busy) agents", map[string]string{})
)
