# ACP Transport (`internal/transport/acp.go`)

基于 IBM BeeAI Agent Communication Protocol 规范，为 Dolphin 新增 ACP Transport，使 Dolphin 能被其他 ACP 兼容 Agent 发现和调用。

## 设计原则

1. **Transport 模式** — 作为与 MQTT/Email/SSH 同级的新 Transport 实现，复用现有 Coordinator + UserIO 接口
2. **遵循标准** — 端点设计直接遵循 BeeAI ACP 规范，不做私有扩展
3. **增量为先** — v1 只做 inbound（接收外部 Agent 请求），v2 扩展 outbound（调用其他 Agent）
4. **自包含** — 不使用外部 SDK，通过标准 Go net/http 实现 REST 端点

## ACP 规范映射

### 实现端点

| 端点 | 方法 | 说明 | v1 |
|------|------|------|:--:|
| `POST /tasks` | POST | 提交任务（sync：等待结果返回；async：返回 task_id） | ✅ |
| `GET /tasks/{task_id}` | GET | 轮询异步任务状态 | ✅ |
| `DELETE /tasks/{task_id}` | DELETE | 取消进行中的任务 | ✅ |
| `GET /tasks/{task_id}/stream` | GET (SSE) | SSE 流式推送任务进度 | ❌ |
| `GET /agents/{agent_id}` | GET | Agent 卡片（元数据） | ✅ |
| `GET /capabilities` | GET | 能力声明 | ✅ |
| `POST /tasks/batch` | POST | 批量提交 | ❌ |

### 任务生命周期

```
PENDING → RUNNING → COMPLETED
                 → FAILED
                 → CANCELLED
```

### 消息格式

遵循 BeeAI ACP 规范的 task 结构。

```json
// POST /tasks 请求体 (Sync — 默认模式)
{
  "id": "0195f1e2-3c4a-7b8d-9e0f-1a2b3c4d5e6f",  // UUIDv7, 客户端生成
  "agentId": "dolphin",
  "sessionId": "session-uuid",
  "task": "Analyze quarterly sales data",
  "context": {
    "contentType": "text/plain",
    "additionalContext": "..."
  },
  "metadata": {
    "timestamp": "2026-05-20T12:00:00Z",
    "priority": "normal"
  }
}

// POST /tasks 请求头 (Async)
// Prefer: respond-async  → 服务器返回 202 + task_id，客户端轮询

// GET /tasks/{id} 响应体
{
  "id": "0195f1e2-3c4a-7b8d-9e0f-1a2b3c4d5e6f",
  "status": "completed",
  "output": {
    "result": "response text",
    "contentType": "text/plain"
  },
  "metadata": {
    "created": "2026-05-20T12:00:00Z",
    "completed": "2026-05-20T12:00:05Z"
  }
}
```

### 任务存储

```go
type acpTask struct {
    ID        string
    Status    string   // pending | running | completed | failed | cancelled
    Input     string
    Output    string
    Err       string
    Created   time.Time
    Completed time.Time
    resultCh  chan string  // sync 模式下 HTTP handler 在此等待
}
```

- 任务存储使用 `sync.Map`（v1，内存级）
- sync 模式下 HTTP handler 在 `resultCh` 上等待（默认 60s 超时）
- async 模式下 handler 立即返回，客户端通过 GET /tasks/{id} 轮询

## Architecture

```
                    ┌──────────────────────────────┐
                    │        Dolphin                │
                    │  ┌─────────────────────────┐  │
                    │  │   ACP HTTP Server        │  │
                    │  │  :8333 (configurable)    │  │
                    │  │                          │  │
                    │  │  POST /tasks     ──────┐ │  │
                    │  │  GET  /tasks/{id}     │ │  │
                    │  │  GET  /capabilities   │ │  │
                    │  │  GET  /agents/{id}    │ │  │
                    │  └──────────────────────│──┘  │
                    │                         │     │
                    │  ┌──────────────────────▼──┐  │
                    │  │    ACP Transport         │  │
                    │  │    (UserIO impl)         │  │
                    │  │                          │  │
                    │  │  msgCh (chan string)     │  │
                    │  │  tasks (sync.Map)        │  │
                    │  └──────────┬───────────────┘  │
                    │             │  ReadLine/Write  │
                    │  ┌──────────▼───────────────┐  │
                    │  │      Coordinator          │  │
                    │  │      (Agent Loop)         │  │
                    │  └──────────────────────────┘  │
                    └──────────────────────────────┘
```

### 请求处理流程 (Sync)

```
Agent A → POST /tasks ─┐
                        ▼
           ACP HTTP Handler
                        │
                    taskStore.Save(task{status: pending})
                        │
                    msgCh ← task.input    (ReadLine 返回)
                        │
                    Agent Loop 处理
                        │
                    WriteLine/output ──→ taskStore.Update(status: completed, result)
                        │
                    HTTP 200 ← result    (等待的 handler 返回)
```

### 请求处理流程 (Async)

```
Agent A → POST /tasks (Prefer: respond-async) ──→ 202 {task_id}
Agent A → GET /tasks/{id} ──→ 200 {status: pending}
                                200 {status: running}
                                200 {status: completed, result}
```

## Config

```yaml
transport:
  acp:
    enabled: true
    listen_addr: ":8333"
    # Agent 身份
    agent_id: "dolphin"
    agent_name: "Dolphin AI Agent"
    agent_version: "0.1"
    agent_description: "Cross-terminal/email/chat/SSH AI agent"
    # 能力声明
    capabilities:
      - "task-execution"
      - "shell-command"
      - "web-search"
      - "browser-automation"
    # 安全
    api_key: ""                    # 空 = 不鉴权
    tls_enabled: false
    tls_cert_file: ""
    tls_key_file: ""
    # 静态对等发现 (v1)
    peers:
      - id: "agent-b"
        url: "http://agent-b:8333"
        api_key: "xxx"
```

## Config 结构体 (Go)

```go
type ACPConfig struct {
    Enabled        bool            `mapstructure:"enabled"`
    ListenAddr     string          `mapstructure:"listen_addr"`
    AgentID        string          `mapstructure:"agent_id"`
    AgentName      string          `mapstructure:"agent_name"`
    AgentVersion   string          `mapstructure:"agent_version"`
    AgentDesc      string          `mapstructure:"agent_description"`
    Capabilities   []string        `mapstructure:"capabilities"`
    SyncTimeout    string          `mapstructure:"sync_timeout"` // default "60s"
    APIKey         string          `mapstructure:"api_key"`
    TLSEnabled     bool            `mapstructure:"tls_enabled"`
    TLSCertFile    string          `mapstructure:"tls_cert_file"`
    TLSKeyFile     string          `mapstructure:"tls_key_file"`
    Peers          []ACP PeerConfig `mapstructure:"peers"`
}

type PeerConfig struct {
    ID     string `mapstructure:"id"`
    URL    string `mapstructure:"url"`
    APIKey string `mapstructure:"api_key"`
}
```

## 文件清单

| 文件 | 说明 |
|------|------|
| `internal/transport/acp.go` | ACP Transport 主实现（Transport + UserIO 接口） |
| `internal/transport/acp_test.go` | 单元测试 |
| `internal/config/config.go` | TransportConfig 增加 ACP 字段 |
| `internal/config/config_test.go` | Config 测试更新 |
| `cmd/root.go` | runActorGroup 中启动 ACP Transport |
| `design/modules/acp-transport.md` | 本文档 |

## 安全

- v1: API Key 通过 `Authorization: Bearer <key>` header 验证
- 空 API Key = 不鉴权（开发/内网环境）
- TLS 可选启用
- OAuth 2.0 留待后续版本

## 版本计划

| 版本 | 范围 | 内容 |
|------|------|------|
| v1 | Inbound | 接收任务、同步/异步响应、能力声明、静态对等、API Key |
| v2 | Outbound | 调用其他 ACP Agent、任务路由、mDNS 发现 |
| v3 | 生产 | OAuth 2.0、持久化任务队列、SSE 流式、OpenTelemetry |

## 风险和缓解

| 风险 | 缓解 |
|------|------|
| BeeAI ACP 规范仍在演进 | 只实现基础稳定端点，用版本头兼容 |
| HTTP 端口冲突 | 默认 8333（BeeAI 默认端口），可配置 |
| 任务堆积 | channel 满时返回 503 Service Unavailable |
| 无鉴权暴露 | 默认禁用，需显式配置 enabled: true 才启动 |

> Last modified: 2026-05-20

## Related

- [Transport Layer](transport.md) — transport interface and existing implementations
- [Architecture Index](../README.md) — master design document index
- [Design Gaps](../gaps.md) — remaining design gaps
- Issue [#187](https://github.com/dolphin/issues/187) — ACP protocol support tracking
