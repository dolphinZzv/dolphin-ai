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
  signUpHuman(email, name, password)
  → 创建 Agent(kind=HUMAN) + 自动创建 Personal Project

方式 B：首次 AI Agent 注册（Bootstrap）
  registerAgent(name, kind=AI, secret, capabilities, bootstrapToken)
  → 验证 bootstrap token → 消耗 → Agent 创建
  → Human Owner 调 addProjectMember 授权

方式 C：已有 Agent 注册新 Agent
  loginAgent → JWT → registerAgent(name, kind=AI, ...)
  → Authorization: Bearer <JWT>
  → addProjectMember(projectId, agentId, role)
```

### 请求认证

| 入口 | 认证方式 | 说明 |
|------|---------|------|
| MCP STDIO | 隐式信任 | 本地进程间通信，无需 token |
| MCP SSE | `Authorization: Bearer <token>` | loginAgent 获取 |
| GraphQL | `Authorization: Bearer <token>` | loginAgent 获取 |
| registerAgent | bootstrapToken | 唯一无认证的端点（首次） |

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
