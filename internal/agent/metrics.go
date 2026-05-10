package agent

import "dolphinzZ/internal/metrics"

// Agent-level metrics collected via the global metrics registry.
var (
	// LLM provider metrics
	llmRequests     = metrics.NewCounter("llm_requests_total", "Total LLM API requests", map[string]string{})
	llmErrors       = metrics.NewCounter("llm_errors_total", "Total LLM API errors", map[string]string{})
	llmDuration     = metrics.NewHistogram("llm_request_duration_seconds", "LLM request duration", map[string]string{}, nil)
	llmInputTokens  = metrics.NewCounter("llm_tokens_total", "Total LLM tokens", map[string]string{})
	llmOutputTokens = metrics.NewCounter("llm_output_tokens_total", "Total LLM output tokens", map[string]string{})

	// Task metrics
	taskDispatched = metrics.NewCounter("agent_tasks_dispatched_total", "Total tasks dispatched to sub-agents", map[string]string{})
	taskCompleted  = metrics.NewCounter("agent_tasks_completed_total", "Total tasks completed by sub-agents", map[string]string{})
	taskFailed     = metrics.NewCounter("agent_tasks_failed_total", "Total sub-agent task failures", map[string]string{})

	// Pool gauge (updated by pool lifecycle)
	agentPoolSize = metrics.NewGauge("agent_pool_size", "Current number of registered agents in pool", map[string]string{})
	activeAgents  = metrics.NewGauge("agent_active_agents", "Current number of active (busy) agents", map[string]string{})
)
