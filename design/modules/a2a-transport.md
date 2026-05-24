# A2A Transport (`internal/transport/a2a/`)

基于 Google Agent-to-Agent (A2A) 协议规范，为 Dolphin 新增 A2A Transport，使 Dolphin 能被其他 A2A 兼容 Agent 发现和调用。

## 设计原则

2. **遵循标准** — 端点设计直接遵循 Google A2A 规范，不做私有扩展
3. **增量为先** — v1 只做 inbound（接收外部 Agent 请求），v2 扩展 outbound（调用其他 Agent）
4. **自包含** — 不使用外部 SDK，通过标准 Go net/http 实现 JSON-RPC 2.0 端点

## A2A 规范映射

### 实现端点

| 端点 | 方法 | 说明 | v1 |
|------|------|------|:--:|
| `POST /a2a` | POST | JSON-RPC 2.0 统一端点，分发到 tasks/send / tasks/get / tasks/cancel | ✅ |
| `GET /.well-known/agent.json` | GET | Agent Card（能力发现） | ✅ |

### JSON-RPC 方法

| 方法 | 说明 | 同步/异步 |
|------|------|:--------:|
| `tasks/send` | 提交任务并等待结果 | 同步（阻塞等待） |
| `tasks/get` | 查询已提交任务的状态 | 异步（立即返回） |
| `tasks/cancel` | 取消进行中的任务 | 异步（立即返回） |

### 任务状态

```
SUBMITTED → WORKING → COMPLETED
                  → FAILED
                  → CANCELED
                  → REJECTED
```

额外状态（v1 预留）：`INPUT_REQUIRED`、`AUTH_REQUIRED`、`REJECTED`

### 消息格式

遵循 Google A2A 规范的 JSON-RPC 2.0 消息结构。

```json
// tasks/send 请求
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tasks/send",
  "params": {
    "id": "0195f1e2-3c4a-7b8d-9e0f-1a2b3c4d5e6f",
    "message": {
      "role": "user",
      "parts": [
        { "type": "text", "text": "Analyze quarterly sales data" }
      ]
    }
  }
}

// tasks/send 响应（completed）
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "0195f1e2-3c4a-7b8d-9e0f-1a2b3c4d5e6f",
    "status": {
      "state": "completed",
      "message": {
        "role": "agent",
        "parts": [
          { "type": "text", "text": "response text" }
        ]
      }
    }
  }
}
```

### 任务存储

```go
type a2aTask struct {
    mu        sync.Mutex
    ID        string
    SessionID string
    Input     string
    State     taskState   // submitted | working | completed | failed | canceled
    Output    string
    Error     string
    Created   time.Time
    Completed time.Time
    resultCh  chan string  // Sync模式下 HTTP handler在这里等待结果
}
```

- 任务存储使用 `sync.Map`（v1，内存级）
- 同步（tasks/send）模式下 HTTP handler 在 `resultCh` 上等待（默认 60s 超时）
- 异步（tasks/get）模式下 handler 立即返回存储的任务状态

## Architecture

```
                    ┌──────────────────────────────┐
                    │        Dolphin                │
                    │  ┌─────────────────────────┐  │
                    │  │   A2A HTTP Server        │  │
                    │  │  :8334 (configurable)    │  │
                    │  │                          │  │
                    │  │  POST /a2a       ──────┐ │  │
                    │  │  GET  /.well-known/   │ │  │
                    │  │       agent.json      │ │  │
                    │  └──────────────────────│──┘  │
                    │                         │     │
                    │  ┌──────────────────────▼──┐  │
                    │  │    A2A Transport         │  │
                    │  │    (UserIO impl)         │  │
                    │  │                          │  │
                    │  │  msgCh (chan *a2aTask)   │  │
                    │  │  tasks (sync.Map)        │  │
                    │  └──────────┬───────────────┘  │
                    │             │  ReadLine/Write  │
                    │  ┌──────────▼───────────────┐  │
                    │  │      Coordinator          │  │
                    │  │      (Agent Loop)         │  │
                    │  └──────────────────────────┘  │
                    └──────────────────────────────┘
```

### 请求处理流程

```
Agent A → POST /a2a (tasks/send)
                        │
                    JSON-RPC decode → dispatch to handleTasksSend
                        │
                    a2aTask{state: submitted} → tasks.Store(id, task)
                        │
                    msgCh ← task        (ReadLine 返回 task.Input)
                        │
                    Agent Loop 处理
                        │
                    WriteLine(result) ──→ task.setCompleted(result)
                        │
                    resultCh ← result   (等待的 handler 收到结果)
                        │
                    HTTP 200 ← result   (JSON-RPC response with completed)
```

## Config

```yaml
transport:
  a2a:
    enabled: true
    listen_addr: ":8334"
    # Agent 身份
    agent_id: "dolphin"
    agent_name: "Dolphin AI Agent"
    agent_version: "0.1"
    agent_description: "Cross-terminal/email/chat/SSH AI agent"
    # 能力声明（agent.json）
    capabilities:
      - "task-execution"
      - "shell-command"
      - "web-search"
    # 安全
    api_key: ""                    # 空 = 不鉴权
    tls_enabled: false
    tls_cert_file: ""
    tls_key_file: ""
    # 同步超时
    sync_timeout: "60s"
```

## Config 结构体 (Go)

```go
type A2AConfig struct {
    Enabled      bool     `mapstructure:"enabled"`
    ListenAddr   string   `mapstructure:"listen_addr"`
    AgentID      string   `mapstructure:"agent_id"`
    AgentName    string   `mapstructure:"agent_name"`
    AgentVersion string   `mapstructure:"agent_version"`
    AgentDesc    string   `mapstructure:"agent_description"`
    Capabilities []string `mapstructure:"capabilities"`
    SyncTimeout  string   `mapstructure:"sync_timeout"`
    APIKey       string   `mapstructure:"api_key"`
    TLSEnabled   bool     `mapstructure:"tls_enabled"`
    TLSCertFile  string   `mapstructure:"tls_cert_file"`
    TLSKeyFile   string   `mapstructure:"tls_key_file"`
}
```

## 文件清单

| 文件 | 说明 |
|------|------|
| `internal/transport/a2a/a2a.go` | A2A Transport 主实现（Transport + UserIO + JSON-RPC endpoints） |
| `internal/transport/a2a/a2a_test.go` | 单元测试（12 tests） |
| `internal/config/config.go` | TransportConfig 增加 A2A 字段 |
| `cmd/root.go` | runActorGroup 中启动 A2A Transport |
| `internal/i18n/i18n.go` + `en.go` + `zh.go` | A2A banner 国际化 |
| `tests/dolphin_test.go` | e2e 集成测试 |
| `design/modules/a2a-transport.md` | 本文档 |

## 安全

- v1: API Key 通过 `Authorization: Bearer <key>` header 验证
- 空 API Key = 不鉴权（开发/内网环境）
- TLS 可选启用（cert_file + key_file 配置）

## 版本计划

| 版本 | 范围 | 内容 |
|------|------|------|
| v1 | Inbound | tasks/send（同步）、tasks/get、tasks/cancel、Agent Card、API Key 鉴权 |
| v2 | Outbound | 调用其他 A2A Agent、多任务并发、异步推送通知 |
| v3 | 生产 | 持久化任务队列、流式响应、mDNS 发现 |

## 风险和缓解

| 风险 | 缓解 |
|------|------|
| Google A2A 规范仍在演进 | 只实现核心 JSON-RPC 方法，保持合约简洁 |
| 任务堆积 | channel 满时 10s 超时返回错误 |
| 无鉴权暴露 | 默认禁用，需显式配置 enabled: true 才启动 |

## 使用方式

### 1. 启用配置

```yaml
transport:
  a2a:
    enabled: true
    listen_addr: ":8334"
```

### 2. 启动 Dolphin

```bash
dolphin
# 输出: === A2A transport active (Google Agent-to-Agent) on :8334 ===
```

### 3. 发送任务

```bash
curl -X POST http://localhost:8334/a2a \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"tasks/send",
    "params":{
      "id":"test-1",
      "message":{"role":"user","parts":[{"type":"text","text":"hello"}]}
    }
  }'
```

### 4. 查询任务

```bash
curl -X POST http://localhost:8334/a2a \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":2,
    "method":"tasks/get",
    "params":{"id":"test-1"}
  }'
```

### 5. 取消任务

```bash
curl -X POST http://localhost:8334/a2a \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0",
    "id":3,
    "method":"tasks/cancel",
    "params":{"id":"test-1"}
  }'
```

### 6. 查看 Agent Card

```bash
curl http://localhost:8334/.well-known/agent.json
```

### 7. 带鉴权

```bash
curl -X POST http://localhost:8334/a2a \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer your-api-key' \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"tasks/send",
    "params":{
      "id":"test-1",
      "message":{"role":"user","parts":[{"type":"text","text":"hello"}]}
    }
  }'
```

> Last modified: 2026-05-22

## Related

- [Transport Layer](transport.md) — transport interface and existing implementations
- [Architecture Index](../README.md) — master design document index
- Google A2A Specification — https://github.com/google/A2A
