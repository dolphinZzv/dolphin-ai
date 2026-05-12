# API 设计

## 1. 双重交付接口

MCP 和 GraphQL 共享 Service 层，是 peer 关系。

```
MCP Tools (AI Agent 入口)         GraphQL (Human / SDK 入口)
--------------------------        ---------------------------
create_issue                      mutation createIssue
add_comment                       mutation addComment
assign_issue                      mutation addAssignee
transition_issue                  mutation transitionIssue
search_issues                     query issues
list_agents                       query agents
check_notifications               subscription agentNotifications
                                  (AI Agent 轮询)       (Human UI WebSocket 推送)
```

详细 Schema 定义见 `internal/graphql/schema.graphqls`（代码即文档）。

## 2. 认证与授权

### Agent 注册

Agent 只能通过 UI（GraphQL）注册，MCP 不提供注册接口：

```
方式 A：人类注册（UI 页面）
  → GraphQL mutation registerAgent(name, kind=human, ...)
  → 创建 Agent，自动生成 Token（32 字节 hex）

方式 B：AI Agent 注册（UI 页面，管理员操作）
  → GraphQL mutation registerAgent(name, kind=ai, ...)
  → 创建 Agent，自动生成 Token（32 字节 hex）
```

注册时自动生成 `Token` 字段，用于 MCP SSE Bearer Token 认证。

### Agent 设备与模型信息

Agent 注册时可携带设备信息和 AI 模型信息，用于管理面展示和调度决策：

| 参数 | 类型 | 说明 |
|------|------|------|
| deviceInfo | String | 设备信息（OS、架构、主机名等） |
| modelInfo | String | AI 模型信息（模型名称、版本等） |

存储于 Agent 模型的 `device_info`（TEXT）和 `model_info`（varchar(255)）字段，GraphQL/MCP 响应中返回。

### 请求认证

| 入口 | 认证方式 | 说明 |
|------|---------|------|
| MCP SSE | `Authorization: Bearer <token>` | Agent Token，SSE 握手时认证，会话绑定 agent 身份 |
| GraphQL | `Authorization: Bearer <JWT>` | loginAgent 获取 JWT |

### MCP SSE 传输协议

标准 MCP SSE Session 握手 + Bearer Token 认证：

```
1. Agent → GET /mcp (Authorization: Bearer <token>)
2. Server → 验证 token → 创建 session → event: endpoint
             data: /mcp/session/{sessionId}
3. Client → POST /mcp/session/{sessionId} (JSON-RPC: initialize, tools/list, tools/call...)
4. 会话绑定 agent 身份，工具调用自动使用该 agent（无需 agentId 参数）
```

OpenCode 配置：

```json
{
  "mcpServers": {
    "chick": {
      "type": "remote",
      "url": "http://47.95.200.101:8080/mcp",
      "enabled": true
    }
  }
}
```

### Project 级别权限

| 角色 | 权限 |
|------|------|
| owner | 全部权限（删除 Project、管理成员角色） |
| maintainer | Issue CRUD、成员查看、Label/Milestone 管理 |
| member | Issue CRUD（仅自己创建的）、Comment 添加 |
| observer | 只读 |

**关键原则：** mutation 不从 Input 读取 `creatorId`/`authorId`/`agentId`，而是从认证上下文获取（MCP 从 SSE 会话，GraphQL 从 JWT）。

## 3. Agent 心跳

| 参数 | 默认值 | 说明 |
|---|---|---|
| HEARTBEAT_INTERVAL | 30s | Agent 心跳间隔 |
| OFFLINE_TIMEOUT | 5min | last_seen_at 超过此时间则判离线 |
| RELEASE_DELAY | 2min | 离线确认后等待时间才释放 |

Agent 每 30 秒调用 `agent_heartbeat()`，系统每分钟扫描超时释放指派。
