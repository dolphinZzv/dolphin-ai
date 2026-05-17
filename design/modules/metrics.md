# Metrics (`internal/metrics/` — v0.4)

## Types

- `Counter` — 累计值 (llm_requests_total)
- `Gauge` — 即时值 (agent_pool_size)
- `Histogram` — 分布 (llm_request_duration_seconds)
- `LabeledCounter` / `LabeledHistogram` — 按 label value 拆分子指标

## Hierarchy

| Level | Metrics | Labels | Location |
|-------|---------|--------|----------|
| Agent | llm_requests/errors/duration, input/output tokens | `provider`, `model` | `agent/metrics.go` + providers |
| Agent | tasks_dispatched/completed/failed | (static) | `agent/metrics.go` |
| Agent | pool_size, active_agents | (static) | `agent/metrics.go` |
| MCP | tool_calls/errors/duration | `tool` (per-tool-name) | `mcp/registry.go` |
| Transport | connections_active | (static) | `transport/metrics.go` |

## Export

`metrics/http.go` — `/metrics` HTTP handler (Prometheus text format)

## Changes in v0.4

- LLM provider metrics: `Counter` → `LabeledCounter` with `provider` dimension
- LLM duration: `Histogram` → `LabeledHistogram` with `provider` dimension
- MCP tool metrics: added `LabeledHistogram` per-tool latency + error counter
- Transport: added `transport_connections_active` gauge

<!-- last-modified: 2026-05-15 -->
