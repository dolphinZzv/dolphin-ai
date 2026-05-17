# Health Check (`internal/health/` — v1.0)

## Endpoint

`GET /health` — 返回 JSON 格式的组件健康状态。

## Response Format

```json
{
  "status": "ok" | "degraded" | "down",
  "components": [
    {
      "name": "mcp_servers",
      "status": "ok" | "error",
      "message": "3/3 servers connected"
    },
    {
      "name": "plugins",
      "status": "ok",
      "message": "plugin manager running"
    }
  ]
}
```

整体 `status` 规则：
- 全部 `ok` → `ok`
- 有 `error` 但非全部 → `degraded`
- 全部 `error` → `down`

## Components

| Component | Check | Source |
|-----------|-------|--------|
| mcp_servers | MCP 外部服务器连接数 > 0 | `cfg.MCP.Servers` |
| plugins | Plugin manager 已初始化 | `pluginMgr` 非 nil |
| cron | Cron manager 已初始化 | `cronMgr` 非 nil |

## Config

```go
type HealthConfig struct {
    Enabled bool   `mapstructure:"enabled"`
    Addr    string `mapstructure:"addr"` // listen address, e.g. ":9091"
}
```

默认监听 `127.0.0.1:9091`。

## Implementation

`internal/health/` 包结构：

```
internal/health/
├── checker.go      // Checker 接口、Component 状态聚合
└── http.go         // HTTP handler 和 Server 创建
```

在 `cmd/root.go` 的 actor group 中启动（与 metrics server 同模式）。

## HTTP Server Integration

```go
if cfg.Health.Enabled {
    mux := http.NewServeMux()
    mux.Handle("/health", health.Handler(checkers...))
    srv := &http.Server{Addr: cfg.Health.Addr, Handler: mux}
    g.Add(func() error { return srv.ListenAndServe() },
        func(err error) { srv.Shutdown(...) })
}
```

<!-- last-modified: 2026-05-15 -->
