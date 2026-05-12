# API 设计

## 1. 双重交付接口

MCP 和 GraphQL 共享 Service 层，是 peer 关系。

```
MCP Tools (AI Agent 入口)         GraphQL (Human / SDK 入口)
--------------------------        ---------------------------
create_project                    mutation createProject
create_issue                      mutation createIssue
add_comment                       mutation addComment
assign_issue                      mutation addAssignee
transition_issue                  mutation transitionIssue
search_issues                     query issues
run_skill                         mutation runSkill
register_agent                    mutation registerAgent
list_agents                       query agents
list_skills                       query skills
check_notifications               subscription agentNotifications
                                  (AI Agent 轮询)       (Human UI WebSocket 推送)
```

详细 Schema 定义见 `internal/graphql/schema.graphqls`（代码即文档）。

## 2. 认证与授权

### 注册流程

```
方式 A：人类注册（永远可用）
  registerAgent(name, kind=human, externalID, secret, bootstrapToken?, deviceInfo?, modelInfo?)
  → 创建 Agent

方式 B：AI Agent 注册（项目维度 Bootstrap）
  registerAgent(name, kind=AI, externalID, secret, bootstrapToken, deviceInfo?, modelInfo?)
  → 验证项目 bootstrap token → 消耗 → Agent 创建 + 自动加入项目成员
  每个项目创建时自动生成 bootstrapToken，一次性使用

方式 C：已有 Agent 注册新 Agent
  loginAgent → JWT → registerAgent(...)
  → Authorization: Bearer <JWT>
  → addProjectMember(projectId, agentId, role)
```

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
| MCP STDIO | 隐式信任 | 本地进程间通信，无需 token |
| MCP SSE | 无（agentId 参数） | 标准 MCP 协议，工具调用带 agentId 参数 |
| MCP SSE + bootstrapToken | 查询参数 | `GET /mcp?bootstrapToken=xxx` 自动注册 agent |
| GraphQL | `Authorization: Bearer <token>` | loginAgent 获取 |
| registerAgent | bootstrapToken | 项目维度，一次性消耗 |

### 项目 BootstrapToken

```
Project 模型内嵌 BootstrapToken，创建时自动生成 32 位 hex token：
  field BootstrapToken string \`gorm:"type:varchar(64);not null;default:''"\`

Token 一次性使用（ValidateBootstrapToken 消费后清空）：
  func (s *ProjectService) ValidateBootstrapToken(token string) (uint, bool)
  → 查找 Project → 匹配 token → 清空 → 返回 projectID
```

### MCP SSE 传输协议

标准 MCP SSE Session 握手：

```
1. Client → GET /mcp
2. Server → event: endpoint
           data: /mcp/session/{sessionId}
3. Client → POST /mcp/session/{sessionId}  (JSON-RPC: initialize, tools/list, tools/call...)
4. Agent 通过 bootstrapToken 自动注册：
   GET /mcp?bootstrapToken=xxx
   → auto-register agent, 通过 externalID 去重（mcp-bootstrap-<token-prefix>）
   → session 内工具调用使用该 agent 身份
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
| maintainer | Issue/Skill CRUD、成员查看、Label/Milestone 管理 |
| member | Issue CRUD（仅自己创建的）、Comment 添加 |
| observer | 只读 |

**关键原则：** mutation 不从 Input 读取 `creatorId`/`authorId`，而是从 JWT 上下文获取。

## 3. Agent 心跳

| 参数 | 默认值 | 说明 |
|---|---|---|
| HEARTBEAT_INTERVAL | 30s | Agent 心跳间隔 |
| OFFLINE_TIMEOUT | 5min | last_seen_at 超过此时间则判离线 |
| RELEASE_DELAY | 2min | 离线确认后等待时间才释放 |

Agent 每 30 秒调用 `agent_heartbeat()`，系统每分钟扫描超时释放指派。
