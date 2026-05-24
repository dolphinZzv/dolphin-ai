# A2A 传输层 — Google Agent-to-Agent 协议

A2A transport 实现了 [Google Agent-to-Agent (A2A) 协议](https://github.com/google/A2A)，使用 JSON-RPC 2.0 over HTTP。它允许其他兼容 A2A 协议的 Agent 发现和调用 Dolphin。

> **当前状态**: 仅 Inbound（v1）。Outbound 调用计划在 v2 中实现。

## 配置

```yaml
transport:
  a2a:
    enabled: true
    listen_addr: ":8334"
    agent_id: dolphin
    agent_name: Dolphin AI Agent
    agent_version: "0.1.0"
    agent_description: "Cross-terminal/email/chat/SSH AI agent"
    capabilities:
      - task-execution
      - shell-command
      - web-search
    sync_timeout: 60s
    api_key: ""                     # 空 = 不鉴权
    tls_enabled: false
    tls_cert_file: ""
    tls_key_file: ""
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 启用 A2A transport |
| `listen_addr` | string | `":8334"` | HTTP 监听地址 |
| `agent_id` | string | `"dolphin"` | 唯一 Agent 标识 |
| `agent_name` | string | `"Dolphin AI Agent"` | 人类可读的 Agent 名称 |
| `agent_version` | string | `"0.1.0"` | Agent 版本 |
| `agent_description` | string | — | Agent 描述 |
| `capabilities` | []string | — | 能力列表（用于发现） |
| `sync_timeout` | string | `"60s"` | 同步任务最大等待时间 |
| `api_key` | string | `""` | Bearer token 鉴权。空 = 不鉴权 |
| `tls_enabled` | bool | `false` | 启用 TLS |
| `tls_cert_file` | string | `""` | TLS 证书文件路径 |
| `tls_key_file` | string | `""` | TLS 密钥文件路径 |

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
```

当看到 banner：`=== A2A transport active (Google Agent-to-Agent) on :8334 ===` 即表示启动成功。

### 3. 查看 Agent Card

```bash
curl http://localhost:8334/.well-known/agent.json
```

### 4. 发送任务

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"test-001","message":{"role":"user","parts":[{"type":"text","text":"几点了?"}]}}}'
```

### 5. 查询或取消任务（可选）

```bash
# 查询任务状态
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"test-001"}}'

# 取消运行中的任务
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"test-001"}}'
```

## API 端点

### `/.well-known/agent.json` (GET)

Agent 能力发现端点。返回该 Agent 的元数据。

```bash
curl http://localhost:8334/.well-known/agent.json
```

### `/a2a` (POST)

JSON-RPC 2.0 端点，用于 A2A 任务操作。

#### JSON-RPC 方法

| 方法 | 说明 | 异步 |
|------|------|:----:|
| `tasks/send` | 提交任务并等待结果 | ❌（同步） |
| `tasks/get` | 查询已提交任务的状态 | ✅ |
| `tasks/cancel` | 取消运行中的任务 | ✅ |

#### tasks/send

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"test-001","message":{"role":"user","parts":[{"type":"text","text":"几点了?"}]}}}'
```

#### tasks/get

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"test-001"}}'
```

#### tasks/cancel

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"test-001"}}'
```

## 鉴权

配置 `api_key` 后，客户端需在请求头中携带：

```
Authorization: Bearer <api_key>
```

`api_key` 为空时所有请求直接放行，不进行鉴权。

## 任务状态

```
submitted → working → completed
                  → failed
                  → canceled
                  → rejected
```

## 已知限制 (v1)

- 仅同步模式 — `tasks/send` 会阻塞 HTTP handler 直到 Agent 响应或超时
- 任务存储在内存中，重启后丢失
- 不支持流式响应
- 不支持推送通知

## 参考

- [Google A2A 规范](https://github.com/google/A2A)
- [设计文档](../../design/modules/a2a-transport.md)

---

> 最后更新: 2026-05-22
