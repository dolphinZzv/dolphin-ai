# Agent Collaboration Platform — 完整设计文档

> 多 Agent 协作系统，人类作为一等 Agent 参与，基于 Go + GraphQL，参考 GitHub Issues / Gitea Issues 模型。

---

## 1. 核心概念

```
                     ┌─────────────────────┐
                     │   Agent Registry    │
                     │  (能力发现/注册)      │
                     └────────┬────────────┘
                              │
                     ┌────────▼────────────┐
                     │     Project         │
                     │  (隔离/权限/配置)     │
                     └────────┬────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   ┌────▼────┐         ┌─────▼─────┐         ┌─────▼────┐
   │ Agent A │◄───────►│  Issues   │◄───────►│  Human   │
   │  (AI)   │         │ (Core     │         │ (Agent)  │
   └────┬────┘         │  Workload)│         └─────┬────┘
        │              └─────┬─────┘               │
   ┌────▼────┐               │                ┌────▼────┐
   │ Agent B │◄──────────────┴──────────────►│  Human  │
   │  (AI)   │                               │ (Agent) │
   └─────────┘                               └─────────┘
```

**核心洞察：** 每个 Issue = 一个工作单元（task/story/bug）。Agent 之间通过 Issue 的 Comment 交流，通过 Assignment 流转任务，通过 Label 匹配能力。

**人与 AI 统一模型：** 对于系统而言，人和 AI Agent 都是 Agent 类型，只是 `kind` 不同。Assignment、Notification、Comment 的代码逻辑完全复用。

**Project = 协作边界：** 所有 Issue、Label、Milestone、Skill 都属于一个 Project。Agent 通过 Project Membership 获得权限。Project 是配置、隔离、权限的基本单元。

---

## 2. 系统架构图

MCP 和 GraphQL 是 peer 关系，共享 Service Layer（MCP 不调 GraphQL）：

```

                         AI Assistant
                      (Claude, GPT, etc.)
                              |
                         MCP Protocol
                              |
                     MCP Client (Host)
                              |
       Platform               |
          |                   ▼
          |     Human UI (React)
          |  shadcn + urql + Vite
          |  [Human 操作界面]
          |
          |   Entry Points (peer, not chain)
          |  +--------------------------+  +--------------------------+
          |  |     GraphQL API          |  |     MCP Server           |
          |  |  (gqlgen + WS)           |  |  (SSE / STDIO)           |
          |  |  [Human UI / SDK入口]     |  |  [AI Assistant 入口]     |
          |  +------------+-------------+  +------------+-------------+
          |               |                             |
          |               |   +---------------------+   |
          |               |   | MCP Tool Handler    |   |
          |               |   | (thin adapter)       |   |
          |               |   | 解析MCP参数调Service |   |
          |               |   +----------+----------+   |
          |               |              |              |
          |  +------------+--------------+--------------+-------------+
          |  |                 Service Layer (共享)                  |
          |  |  +---------+ +---------+ +---------+ +-------------+ |
          |  |  | Project | | Issue   | | Agent   | | Skill       | |
          |  |  | Service | | Service | | Registry| | Engine      | |
          |  |  +---------+ +---------+ +---------+ +-------------+ |
          |  |  +---------+ +---------+ +---------+                |
          |  |  |Workflow | | Event   | | Notif   |                |
          |  |  | Engine  | | Bus     | | Service |                |
          |  |  +---------+ +---------+ +---------+                |
          |  +---------------------------+-------------------------+
          |                              |
          |  +---------------------------v-------------------------+
          |  |                     Data Layer                      |
          |  |  PostgreSQL (in-memory EventBus)                            |
          |  |  projects, agents, issues, comments, labels,        |
          |  |  project_members, timeline_events, skill_definitions|
          |  +-----------------------------------------------------+
          +---------------------------------------------------------+
```

**MCP 和 GraphQL 调用同一份 Service：**
MCP Tool Handler 和 GraphQL Resolver 都是薄适配层，职责只做参数解析 → 调用 Service → 格式化返回，不包含任何业务逻辑。

**双重交付接口对照：**

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

MCP Resources                     GraphQL Queries
---------------                   ---------------
project://{id}                    query project(id)
issue://{project}/{number}        query issue(number)
agent://{id}                      query agent(id)
                                  query search(q)
```
### 2.1 认证与授权

#### 注册流程（终版）

```
┌─────────────────────────────────────────────────────────────┐
│  方式 A：人类注册（永远可用）                                 │
│  Human（kind=HUMAN）通过 GraphQL 注册，不需要 bootstrap      │
│  不需要已有 Agent，不需要 token                               │
└─────────────────────────────────────────────────────────────┘

  # 人类注册（唯一无认证端点）
  signUpHuman(email: "user@company.com",
              name: "Zhang San",
              password: "xxx")
  → 创建 Agent(kind=HUMAN) + 自动创建 Personal Project + owner

  # 后续登录
  loginAgent(externalId: "user@company.com", secret: "xxx")
  → JWT (agent_id, project_roles)


┌─────────────────────────────────────────────────────────────┐
│  方式 B：首次 AI Agent 注册 — Bootstrap                     │
│  当数据库中没有 AI Agent 时，需要 bootstrap token            │
│  （防止任意 AI 自注册到陌生人的项目里）                       │
└─────────────────────────────────────────────────────────────┘

  # server 启动时生成 BOOTSTRAP_TOKEN（stdout 或环境变量预设）
  registerAgent(name: "Claude-1",
                kind: AI,
                secret: "...",
                capabilities: [CODING],
                bootstrapToken: "xxxx")       # 唯一不验 JWT 的 AI 入口

  → 验证 bootstrap token → 消耗 → Agent 创建
  → Human Owner 调 addProjectMember 授权


┌─────────────────────────────────────────────────────────────┐
│  方式 C：已有 Agent 注册新 Agent（人管 / AI 管都行）         │
│  任何已有 Agent（Human 或 AI）持有 JWT 都可注册新 Agent       │
└─────────────────────────────────────────────────────────────┘

  # Human 创建 AI Agent
  loginAgent(externalId: "user@company.com", secret: "xxx")
  → JWT

  registerAgent(name: "Claude-Coder",
                kind: AI,
                secret: "生成的密码",
                capabilities: [CODING, REVIEW])
  ← Authorization: Bearer <Human JWT>

  addProjectMember(projectId: "proj-1",
                   agentId: "claude-coder-id",
                   role: member)

  # AI Agent 自动创建其他 Agent（Skill 编排）
  run_skill("scale-team", params: { count: 3 })
    → registerAgent(name: "Worker-{n}", kind: AI, ...)
    → addProjectMember(...)
```

#### 登录与请求认证

```
loginAgent(externalId, secret)
  → 验证密码 → 签发 JWT (含 agent_id, project_roles)
  → 返回 { token, agent }

后续每请求:
  Header: Authorization: Bearer <token>
  → 中间件解析 JWT → 注入 context
  → Handler/Service 从 context 取当前身份
```

#### 认证方式

| 入口 | 认证方式 | 说明 |
|------|---------|------|
| MCP STDIO | 隐式信任 | 本地进程间通信，无需 token |
| MCP SSE | `Authorization: Bearer <token>` | loginAgent 获取 |
| GraphQL | `Authorization: Bearer <token>` | loginAgent 获取 |
| **registerAgent** | bootstrapToken | 唯一无认证的端点（首次） |

#### Project 级别权限

| 角色 | 权限 |
|------|------|
| owner | 全部权限（删除 Project、管理成员角色） |
| maintainer | Issue/Skill CRUD、成员查看、Label/Milestone 管理 |
| member | Issue CRUD（仅自己创建的）、Comment 添加 |
| observer | 只读 |

**关键变更：** mutation 不再从 Input 中读取 creatorId/authorId，而是从认证上下文获取。

```
❌ 旧: createIssue(input: { creatorId: "1", ... })  // 客户端可伪造
✅ 新: createIssue(input: { title: "...", ... })    // 从 token 取 creator
```

### 2.2 Agent 心跳与超时释放

Agent 心跳用于检测 Agent 是否活跃，超时机制防止 Issue 被废弃。

```
Agent 心跳流程:

  1. Agent 每 30 秒调用 heartbeat() API / MCP tool
     更新 Agent.last_seen_at

  2. 系统定时扫描（每分钟）:
     SELECT * FROM issue_assignees
     WHERE state IN ('pending', 'in_progress')
       AND EXISTS (
         SELECT 1 FROM agents
         WHERE agents.id = issue_assignees.agent_id
         AND agents.last_seen_at < NOW() - INTERVAL '5 minutes'
       )

  3. 超时处理:
     -> 将 IssueAssignee 状态重置为 PENDING
     -> 发布 IssueAssignee.Timeout 事件
     -> 通知 Project Owner
     -> 触发重新匹配
```

| 参数 | 默认值 | 说明 |
|---------|---------|------|
| HEARTBEAT_INTERVAL | 30s | Agent 心跳间隔 |
| OFFLINE_TIMEOUT | 5min | last_seen_at 超过此时间则判离线 |
| RELEASE_DELAY | 2min | 离线确认后等待时间才释放 |

**MCP Tool:** `agent_heartbeat()` -- Agent 定时调用更新心跳，无参数。

---

### 2.3 代码分层设计

#### 三层架构总览

```
┌─────────────────────────────────────────────────────────────┐
│                    Handler Layer                             │
│  ┌─────────────────────────┐  ┌──────────────────────────┐  │
│  │ graph/resolver/         │  │ mcp/tools/               │  │
│  │  - project.go           │  │  - project.go            │  │
│  │  - issue.go             │  │  - issue.go              │  │
│  │  - agent.go             │  │  - agent.go              │  │
│  │  - comment.go           │  │  - comment.go            │  │
│  │  - subscription.go      │  │  - skill.go              │  │
│  └──────────┬──────────────┘  └───────────┬──────────────┘  │
│             职责: 协议适配、参数解析、调用Service              │
│             禁止: 写业务逻辑、直接操作DB                       │
└──────────────┼────────────────────────────┼──────────────────┘
               │                            │
┌──────────────▼────────────────────────────▼──────────────────┐
│                      Service Layer                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐  │
│  │ project  │  │  issue   │  │  agent   │  │  matching   │  │
│  │ service  │  │  service │  │  service │  │  engine     │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └──────┬──────┘  │
│  ┌────┴─────┐  ┌────┴─────┐  ┌────┴─────┐  ┌──────┴──────┐  │
│  │ workflow │  │  event   │  │  notif   │  │  skill      │  │
│  │ engine   │  │  bus     │  │  service │  │  registry   │  │
│  └──────────┘  └──────────┘  └──────────┘  └─────────────┘  │
│             职责: 业务编排、状态机、审批流、匹配算法              │
│             依赖: Repository 接口（不是具体实现）               │
└──────────────────────────┬───────────────────────────────────┘
                           │
┌──────────────────────────▼───────────────────────────────────┐
│                    Repository Layer                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐  │
│  │ project  │  │  issue   │  │  agent   │  │  comment    │  │
│  │  repo    │  │  repo    │  │  repo    │  │  repo       │  │
│  └──────────┘  └──────────┘  └──────────┘  └─────────────┘  │
│             职责: GORM CRUD、复杂查询、事务管理                 │
│             依赖: *gorm.DB                                     │
└──────────────────────────────────────────────────────────────┘

依赖方向（严格单向）:
  Handler  →  Service  →  Repository  →  DB
  (上层)        (中层)       (下层)
```

#### 每层职责详述

**Handler 层（internal/graph/resolver/ + internal/mcp/tools/）**

| 职责 | 说明 |
|---|---|
| 协议适配 | GraphQL resolver 处理 query/mutation/subscription；MCP handler 处理 tool/resource/prompt |
| 参数解析 | 从 GraphQL Input 或 MCP JSON 参数中提取数据，转为 Service 层可用的结构 |
| 调用 Service | 持有 Service 接口引用，每个 handler 方法只做"解析 → 调用 → 返回" |
| 错误格式化 | 将 Service 层的领域错误转为 GraphQL error 或 MCP error response |
| ❌ 禁止做的事 | 写业务逻辑、直接调 Repository、操作 DB、维护状态 |

**Service 层（internal/service/ + internal/matching/ + internal/events/ + internal/skill/）**

| 模块 | 文件 | 核心职责 |
|---|---|---|
| **project** | service/project.go | 创建/更新 Project、管理成员、角色校验 |
| **issue** | service/issue.go | 创建 Issue、状态流转、Assignee 变更、父子 Issue 维护 |
| **agent** | service/agent.go | Agent 注册/状态管理、能力声明、心跳 |
| **workflow** | service/workflow.go | 状态机定义（state transition rules）、审批流调度 |
| **matching** | matching/router.go | Label → Capability → Agent 匹配算法、自动指派 |
| **events** | events/bus.go | 事件发布/订阅（in-memory → NATS）、领域事件路由 |
| **notifications** | notifications/service.go | 指派通知、@提及、审批请求推送 |
| **skill** | skill/registry.go | Skill 定义存储、YAML 解析、MCP Tool 编排执行 |

Service 层关键设计原则：
- 每个 Service 面向 Repository 接口编程，依赖通过构造函数注入
- Service 之间可以相互调用（如 issue service 调用 matching engine）
- 业务操作完成后发布领域事件（Issue.Created、Issue.StateChanged 等）

**Repository 层（internal/repository/）**

| 文件 | GORM 操作 | 说明 |
|---|---|---|
| project.go | Create, GetByID, Update, Delete, ListMembers | Project + ProjectMember |
| issue.go | Create, GetByID, ListByProject, UpdateState, AddLabels | Issue + 多对多 Label |
| agent.go | Create, GetByID, FindByCapability, UpdateStatus | Agent 查询 |
| comment.go | Create, GetByIssueID, GetByParentID | Comment 树状查询 |

Repository 层关键设计原则：
- 每个 Repository 封装一张或一组关联表的 GORM 操作
- 复杂查询（联表过滤、分页、排序）在 Repository 层完成
- Repository 接口在 Service 层定义，实现在 Repository 层

#### 数据流

```
用户创建 Issue 的数据流:

  Handler (GraphQL/MCP)
    │ 1. 解析入参 { title, projectId, labels }
    ▼
  Service (issue.Service.Create)
    │ 2. 参数校验、构建 Issue 对象
    │ 3. 调用 matching engine 查合适 Agent
    │ 4. 调用 Repository 写入 DB
    │ 5. 发布 Issue.Created 事件到 EventBus
    ▼
  Repository (issueRepo.Create)
    │ 6. GORM INSERT
    ▼
  DB

  EventBus 异步触发:
    │
    ├──→ matching.engine: 自动指派 Agent → issueService.Assign()
    │
    └──→ notification.service: 通知 Agent

  Handler 返回 Issue 给调用方
```

#### 包依赖规则

```
internal/
├── graph/resolver/     → 只依赖 service/ 和 matching/
├── mcp/tools/          → 只依赖 service/ 和 matching/
├── skill/              → 只依赖 service/ 和 matching/
├── service/            → 依赖 repository/（接口）和 events/
├── events/             → 依赖 notification/（接口）
├── matching/           → 依赖 repository/（接口）
├── notifications/      → 依赖 repository/（接口）
├── repository/         → 只依赖 models/ 和 *gorm.DB
└── models/             → 纯数据结构，无依赖

禁止的依赖（违反会被 code review 拒绝）:
  ❌ resolver → repository       (越层调用)
  ❌ mcp/tools → models          (绕过 Service)
  ❌ service → graph/resolver    (反向依赖)
  ❌ repository → service         (循环依赖)
```

#### Repository 接口定义（支持 ORM 替换）

Repository 层通过接口暴露，Service 层只依赖接口不依赖实现，底层 ORM 可随时替换。

```
// ─── 接口定义（在 service 层或独立的 repository 包中） ───

ProjectRepository:
  Create(project) → (Project, error)
  GetByID(id) → (Project, error)
  Update(id, changes) → error
  Delete(id) → error
  AddMember(projectId, agentId, role) → error
  RemoveMember(projectId, agentId) → error
  ListMembers(projectId) → []ProjectMember

IssueRepository:
  Create(issue) → (Issue, error)
  GetByID(id) → (Issue, error)
  GetByNumber(projectId, number) → (Issue, error)
  ListByProject(projectId, filter) → ([]Issue, error)
  UpdateState(id, state) → error
  UpdateAssignee(id, agentId) → error
  AddLabels(id, labelIds) → error
  RemoveLabels(id, labelIds) → error
  Transaction(fn) → error

AgentRepository:
  Create(agent) → (Agent, error)
  GetByID(id) → (Agent, error)
  UpdateStatus(id, status) → error
  FindByCapability(capabilities, projectId) → ([]Agent, error)
  FindOnlineByProject(projectId) → ([]Agent, error)

CommentRepository:
  Create(comment) → (Comment, error)
  GetByIssueID(issueId) → ([]Comment, error)
  GetByParentID(parentId) → ([]Comment, error)
```

**替换 ORM 的方式：**

```
当前: GORM 实现
  repository/issue.go  →  type IssueRepo struct{ db *gorm.DB }
                         func (r *IssueRepo) Create(issue) { ... gorm 操作 ... }

替换为 XORM/ sqlx 时:
  1. 新建 repository/sqlx/issue.go → 实现同一套 IssueRepository 接口
  2. main.go 中替换注入:  repository.NewIssueRepo(db)
                                ↓
                          repository/sqlx.NewIssueRepo(db)
  3. Service 层零改动

关键: Repository 接口 + 构造函数注入 = ORM 可替换
```

数据流验证：

```
GORM 路径:  Handler → Service → IssueRepository(接口) → IssueRepo(GORM实现) → *gorm.DB → PostgreSQL
替换后:    Handler → Service → IssueRepository(接口) → IssueRepo(sqlx实现) → *sqlx.DB → PostgreSQL
                    ↑ 这层不动          ↑ 接口不变         ↑ 只换这里               ↑ 换驱动
```


#### 数据库多驱动支持

GORM 原生支持 SQLite + PostgreSQL 双驱动，DSN 切换，Service/Repository 层零改动。

**切换方式：**

```
开发: driver: sqlite3     dsn: "data/dev.db"
生产: driver: postgres    dsn: "postgres://user:pass@host:5432/db"

环境变量: DB_DRIVER=sqlite3 DB_DSN="data/dev.db" ./server
```

**驱动差异：**

| 特性 | SQLite | PostgreSQL | 处理 |
|---|---|---|---|
| 自增 ID | INTEGER PK | SERIAL | GORM 自动 |
| JSON | TEXT(序列化) | jsonb | GORM serializer |
| 并发 | 写锁 | MVCC | 开发OK, 生产PG |
| 迁移 | AutoMigrate | goose | 开发自动, 生产手动 |

**测试策略：**

```
单元测试:   SQLite :memory: 模式，快速运行无依赖
集成测试:   PostgreSQL (testcontainers)，验证真实 SQL/索引
CI 流水线:  SQLite + PostgreSQL 双驱动并行运行
```
### 2.4 技术选型

| 层 | 技术 | 理由 |
|---|---|---|
| 语言 | Go 1.22+ | 强并发、编译快、部署简单 |
| API | GraphQL (gqlgen) | 强类型 Schema、Subscription、灵活查询 |
| ORM | GORM | 支持 SQLite + PostgreSQL 双驱动, DSN 切换 |
| DB | SQLite (dev) / PostgreSQL (prod) | SQLite 本地快速开发, PG 生产级 JSONB + 完整 ACID |
| 迁移 | AutoMigrate + goose | AutoMigrate 开发用, goose 管理生产迁移 |
| 消息总线 | in-memory (可选 NATS) | Phase1-3 单实例够用, Phase4 可换 NATS |

---
## 3. GraphQL Schema 完整设计

### 3.1 Agent 类型

```graphql
enum AgentKind {
  AI       # AI Agent
  HUMAN    # 人类
  HYBRID   # 人+AI 协作
}

enum AgentStatus {
  ONLINE
  BUSY
  OFFLINE
  ERROR
}

enum CapabilityType {
  CODE_REVIEW
  CODING
  TESTING
  DEVOPS
  DESIGN
  DOCUMENTATION
  ANALYSIS
  MANAGEMENT
}

type Agent {
  id: ID!
  name: String!
  kind: AgentKind!
  status: AgentStatus!
  externalId: String                   # email / model identifier
  systemPrompt: String                 # AI Agent instructions
  capabilities: [CapabilityType!]!
  metadata: JSONObject                 # model, provider, timezone 等
  assignedIssues: [Issue!]!
  createdIssues: [Issue!]!
  comments: [Comment!]!
  createdAt: Time!
  updatedAt: Time!
  lastSeenAt: Time
}
```

### 3.2 Project 类型 — 协作边界

```graphql
"""Project = 隔离单元。Issue/Label/Milestone/Skill 都属于一个 Project。"""
type Project {
  id: ID!
  name: String!
  description: String!

  """Project 内的 Agent 成员 (Human + AI)"""
  members: [ProjectMember!]!

  """Project 内的所有 Issue"""
  issues: [Issue!]!

  """Project 内的标签集"""
  labels: [Label!]!

  """Project 内的里程碑"""
  milestones: [Milestone!]!

  """Project 内的 Skill 定义"""
  skills: [Skill!]!

  createdAt: Time!
  updatedAt: Time!
}

"""Agent 在 Project 中的成员身份"""
type ProjectMember {
  id: ID!
  project: Project!
  agent: Agent!
  role: ProjectRole!
  joinedAt: Time!
}

enum ProjectRole {
  OWNER         # 完全控制
  MAINTAINER    # 管理 Issue/Label/Milestone + 管理成员
  MEMBER        # 创建/处理 Issue
  OBSERVER      # 只读
}

"""Agent 的跨 Project 成员关系"""
extend type Agent {
  memberships: [ProjectMember!]!
}
```

Issue 中引用 Project：

```graphql
type Issue {
  id: ID!
  number: Int!                        # Project 内自增短编号 #123
  title: String!
  description: String!                # Markdown
  state: IssueState!
  priority: Priority!
  project: Project!                   # 所属 Project
  creator: Agent!
  assignees: [IssueAssignee!]!
  labels: [Label!]!
  parent: Issue                       # 父 Issue（树状分解）
  children: [Issue!]!                 # 子 Issue
  related: [Issue!]!                  # 跨引用
  comments: [Comment!]!
  timeline: [TimelineEvent!]!
  milestone: Milestone
  dueDate: Time
  structuredOutput: JSONObject        # Agent 结构化输出
  createdAt: Time!
  updatedAt: Time!
  closedAt: Time
}
```

### 3.3 Comment 类型

```graphql
enum CommentContentType {
  MARKDOWN       # 文本
  TOOL_CALL      # Agent 调用了工具
  TOOL_RESULT    # 工具执行结果
  CODE_DIFF      # 代码变更
  DECISION       # 决策记录
  APPROVAL       # 审批通过
  REJECTION      # 驳回
  STRUCTURED     # 结构化数据 JSON
}

type Comment {
  id: ID!
  issue: Issue!
  author: Agent!
  parent: Comment                    # 回复关系
  replies: [Comment!]!
  body: String!
  contentType: CommentContentType!
  toolCall: ToolCall                 # TOOL_CALL 时携带
  structuredData: JSONObject         # STRUCTURED 时携带
  metadata: JSONObject
  createdAt: Time!
  updatedAt: Time!
}

type ToolCall {
  toolName: String!
  arguments: JSONObject!
  result: JSONObject
  duration: Duration
  status: ToolCallStatus!
}

enum ToolCallStatus {
  PENDING
  RUNNING
  SUCCESS
  FAILED
}
```

### 3.4 IssueAssignee 多指派状态

```graphql
enum AssigneeState {
  PENDING        # 等待处理
  IN_PROGRESS    # 正在处理
  COMPLETED      # 已完成
  BLOCKED        # 阻塞（等待其他人或外部）
}

type IssueAssignee {
  id: ID!
  issue: Issue!
  agent: Agent!
  state: AssigneeState!
  assignedAt: Time!
  completedAt: Time
}
```

type Issue 中通过 `assignees: [IssueAssignee!]!` 引用。

### 3.5 Timeline 事件溯源

```graphql
union TimelineEvent =
  | IssueCreated
  | IssueStateChanged
  | IssueAssigned
  | IssueUnassigned
  | IssueLabelAdded
  | IssueLabelRemoved
  | CommentAdded
  | AgentStatusChanged
  | MilestoneChanged
  | PriorityChanged

type IssueCreated implements TimelineEvent {
  id: ID!; createdAt: Time!; actor: Agent!; issue: Issue!
}

type IssueStateChanged implements TimelineEvent {
  id: ID!; createdAt: Time!; actor: Agent!
  fromState: IssueState!; toState: IssueState!
}

type IssueAssigned implements TimelineEvent {
  id: ID!; createdAt: Time!; actor: Agent!; assignee: Agent!
}
```

### 3.6 Label 与 Milestone — 属于 Project

```graphql
type Label {
  id: ID!
  name: String!
  color: String!
  description: String
  project: Project!                   # 所属 Project
  capability: CapabilityType          # 映射到 Agent 能力
  group: String                       # 分组: area/, kind/, lang/
  createdAt: Time!
}

type Milestone {
  id: ID!
  title: String!
  description: String
  project: Project!                   # 所属 Project
  dueDate: Time
  state: MilestoneState!
  issues: [Issue!]!
  progress: Progress!
  createdAt: Time!
}

type Progress { total: Int!; completed: Int!; percentage: Float! }
enum MilestoneState { OPEN CLOSED }
```

### 3.7 Query

```graphql
"""分页请求"""
input PageInput {
  limit: Int = 20                        # 每页条数 (max 100)
  offset: Int = 0                        # 偏移量
}

"""分页响应"""
type PageInfo {
  total: Int!                            # 总条数
  hasMore: Boolean!                      # 是否有下一页
  limit: Int!
  offset: Int!
}

type Query {
  # Project
  project(id: ID!): Project!
  projects(page: PageInput): [Project!]!

  # Issue
  issue(id: ID, number: Int): Issue
  issues(filter: IssueFilter, orderBy: IssueOrder, page: PageInput): IssuePage!

  # Agent
  agent(id: ID!): Agent
  agents(filter: AgentFilter, page: PageInput): AgentPage!

  # 辅助
  labels(projectId: ID!, group: String): [Label!]!
  milestones(projectId: ID!, state: MilestoneState): [Milestone!]!

  # 搜索
  search(projectId: ID!, query: String!, page: PageInput): IssuePage!
}

type IssuePage {
  items: [Issue!]!
  pageInfo: PageInfo!
}

type AgentPage {
  items: [Agent!]!
  pageInfo: PageInfo!
}

input IssueFilter {
  projectId: ID
  state: [IssueState!]
  assigneeIds: [ID!]
  labelIds: [ID!]
  milestoneId: ID
  priority: Priority
  creatorId: ID
  search: String
}

input IssueOrder {
  field: IssueOrderField! = CREATED_AT
  direction: OrderDirection! = DESC
}

enum IssueOrderField {
  CREATED_AT
  UPDATED_AT
  PRIORITY
  NUMBER
}

enum OrderDirection {
  ASC
  DESC
}

input AgentFilter {
  kind: AgentKind
  status: AgentStatus
  capabilities: [CapabilityType!]
  projectId: ID
}

### 3.8 Mutation

```graphql
type Mutation {
  # Auth
  """Agent 登录，返回 session token（Human/AI 均可用）"""
  loginAgent(externalId: String!, secret: String!): AuthPayload!

  # Project
  createProject(input: CreateProjectInput!): Project!
  updateProject(id: ID!, input: UpdateProjectInput!): Project!
  deleteProject(id: ID!): Boolean!
  addProjectMember(projectId: ID!, agentId: ID!, role: ProjectRole!): ProjectMember!
  updateProjectMember(projectId: ID!, agentId: ID!, role: ProjectRole!): ProjectMember!
  removeProjectMember(projectId: ID!, agentId: ID!): Boolean!

  # Issue
  createIssue(input: CreateIssueInput!): Issue!
  updateIssue(id: ID!, input: UpdateIssueInput!): Issue!
  deleteIssue(id: ID!): Boolean!
  transitionIssue(id: ID!, toState: IssueState!): Issue!

  # Assignee
  addAssignee(issueId: ID!, agentId: ID!): Issue!
  removeAssignee(issueId: ID!, agentId: ID!): Issue!
  updateAssigneeState(issueId: ID!, agentId: ID!, state: AssigneeState!): IssueAssignee!

  # Comment
  addComment(input: AddCommentInput!): Comment!
  updateComment(id: ID!, body: String!): Comment!
  deleteComment(id: ID!): Boolean!

  # Label / Milestone
  addLabels(issueId: ID!, labelIds: [ID!]!): Issue!
  removeLabels(issueId: ID!, labelIds: [ID!]!): Issue!
  createLabel(input: CreateLabelInput!): Label!
  createMilestone(input: CreateMilestoneInput!): Milestone!

  # Agent
  registerAgent(input: RegisterAgentInput!): Agent!
  updateAgentStatus(id: ID!, status: AgentStatus!): Agent!
}

"""认证返回"""
type AuthPayload {
  token: String!
  agent: Agent!
}
```

input CreateProjectInput {
  name: String!
  description: String!
}

input UpdateProjectInput {
  name: String
  description: String
}

input CreateIssueInput {
  projectId: ID!                       # 必须指定 Project
  title: String!
  description: String!
  creatorId: ID!
  assigneeIds: [ID!]
  labelIds: [ID!]
  priority: Priority = MEDIUM
  parentId: ID
  milestoneId: ID
  dueDate: Time
}

input UpdateIssueInput {
  title: String
  description: String
  assigneeIds: [ID!]
  labelIds: [ID!]
  priority: Priority
  milestoneId: ID
  dueDate: Time
  state: IssueState
}

input AddCommentInput {
  issueId: ID!
  authorId: ID!
  body: String!
  contentType: CommentContentType = MARKDOWN
  parentId: ID
  toolCall: ToolCallInput
  structuredData: JSONObject
}

input RegisterAgentInput {
  name: String!
  kind: AgentKind!
  externalId: String!
  secret: String!                      # API密钥 / 密码（哈希存储）
  systemPrompt: String
  capabilities: [CapabilityType!]!
  metadata: JSONObject
}
```

### 3.9 Subscription — 实时通知（Human UI 专用）

> **设计决策：** AI Agent 不走 WebSocket Subscription，改用 MCP Tool 轮询。
> Human UI 使用 GraphQL Subscription 获得实时推送。

```graphql
type Subscription {
  """监听单个 Issue 的实时更新"""
  issueUpdated(issueId: ID!): IssueEvent!

  """Agent 的个人通知流"""
  agentNotifications(agentId: ID!): Notification!

  """全局 Agent 状态变更"""
  agentStatusChanged: AgentStatusEvent!
}

type IssueEvent {
  type: IssueEventType!
  issue: Issue!
  actor: Agent!
  timestamp: Time!
}

enum IssueEventType {
  COMMENT_ADDED    # 新评论，Agent 可响应
  STATE_CHANGED    # Issue 状态变化
  ASSIGNEE_CHANGED # 处理人变化
  MENTIONED        # 被 @提及
}

type Notification {
  id: ID!
  type: NotificationType!
  issue: Issue
  comment: Comment
  from: Agent
  message: String!
  createdAt: Time!
}

enum NotificationType {
  ISSUE_ASSIGNED
  COMMENT_MENTION
  STATUS_CHANGE_REQUEST
  APPROVAL_REQUESTED
  WORKFLOW_TRIGGER
}
```

### 3.10 Feedback — 用户反馈收集

> 人类和 Agent 可对 Issue 完成质量、协作过程、Agent 表现等提供反馈。
> 反馈用于能力匹配排序、Agent 质量评估、系统改进。

```graphql
enum FeedbackTargetType {
  ISSUE           # 对整个 Issue 完成质量的反馈
  COMMENT         # 对某条评论/工具的反馈
  AGENT           # 对某个 Agent 协作表现的反馈
  ASSIGNMENT      # 对某个指派任务的反馈 (IssueAssignee)
}

enum FeedbackRating {
  ONE
  TWO
  THREE
  FOUR
  FIVE
}

input FeedbackDimension {
  dimension: String!        # 维度: accuracy / speed / communication / code_quality / overall
  rating: FeedbackRating!
}

type Feedback {
  id: ID!
  targetType: FeedbackTargetType!
  targetId: ID!                               # IssueID / CommentID / AgentID / IssueAssigneeID
  author: Agent!                              # 反馈者（人类或 AI）
  rating: FeedbackRating!
  dimensions: [FeedbackDimension!]            # 多维度评分
  body: String                                # 文本反馈
  createdAt: Time!
}

input CreateFeedbackInput {
  targetType: FeedbackTargetType!
  targetId: ID!
  authorId: ID!
  rating: FeedbackRating!
  dimensions: [FeedbackDimension!]
  body: String
}

type Mutation {
  # ... 已有 Mutation ...
  createFeedback(input: CreateFeedbackInput!): Feedback!
}
```

**MCP Tool 映射：**
| MCP Tool | 用途 |
|----------|------|
| `submit_feedback` | Agent/人类提交反馈（含评分+文本+维度） |
| `list_feedback` | 查询指定目标的反馈列表 |

**反馈用途：**
- Agent 的 `feedback_rating` 均值影响能力匹配排序（分数越高优先匹配）
- Project 级别的反馈报表用于衡量协作质量
- Issue 完成后的 Feedback 作为 Phase 3 Skill 优化的输入

---

## 4. 核心工作流

### 4.1 Agent 能力匹配

```
Issue Created with label "bug", "lang:go"
         │
         ▼
  ┌─────────────────┐
  │  Label Router   │── 匹配 Label → Capability
  └────────┬────────┘
           │
           ▼
  ┌─────────────────┐
  │  Capability     │── 查 Agent: capabilities @> ['CODING']
  │  Matcher        │    AND status = 'ONLINE'
  └────────┬────────┘
           │
     ┌─────┴─────┐
     ▼           ▼
  匹配成功     匹配失败（无可用 Agent）
     │           │
     ▼           ▼
 通知Agent    ┌──────────────┐
              │  兜底策略     │
              ├──────────────┤
              │ 1. 入等待队列  │── 每 N 分钟重试匹配
              │ 2. 通知项目Owner│── addComment 手动指派
              │ 3. T+30min 无人 │── Issue 标记 blocked + 通知 Owner
              │    接手则标记   │
              └──────────────┘
```

**兜底策略细节：**

| 触发条件 | 动作 | 说明 |
|----------|------|------|
| 匹配结果为 0 个 Agent | 入等待队列 | `pending_matches` 表记录，定时器每 5 分钟重试 |
| 等待 > 15 分钟 | 通知 Project Owner | addComment @Owner："Issue #N 无人认领" |
| 等待 > 30 分钟 | 标记 BLOCKED + 升级通知 | Issue.state = blocked，通知所有 Project Member |
| Owner 手动指派 | 跳过匹配直接 assign | addAssignee(issueId, agentId) 绕过匹配引擎 |

### 4.2 Agent 间通信

```
Agent A (reviewer)
  │  addComment(issue, body, contentType: REVIEW)
  │
  ├──► Mutation → DB → Event Bus
  │                          │
  │                    Subscription: issueUpdated
  │                          │
  │                    Agent B 收到通知
  ◄───────────────────────────┘
  Agent B: addComment(issue, body, contentType: TOOL_RESULT)
```

### 4.3 Issue 生命周期

```
OPEN ──► IN_PROGRESS ──► REVIEW ──► CLOSED_COMPLETED
 │                            │
 │                            ▼
 ▼                      CLOSED_NOT_PLANNED
BLOCKED ──► IN_PROGRESS
```

### 4.4 Human-in-the-loop 审批

```
Agent A (coding agent)
  transitionIssue(id, REVIEW)
  addComment(body: "PR ready, please review")
         │
         ▼
Human / Review Agent 收到 Notification
  ├── APPROVAL → transitionIssue(id, CLOSED_COMPLETED)
  └── REJECTION → transitionIssue(id, IN_PROGRESS) + comment
```

---


### 4.5 端到端交互图

#### 场景 1：发布任务 → 自动匹配 Agent → 完成

```mermaid
sequenceDiagram
    participant H as Human/Agent
    participant P as Platform
    participant M as Matching Engine
    participant A as AI Agent
    participant N as Notification

    H->>P: createIssue(project, title, labels)
    activate P
    P->>P: 创建 Issue (state=OPEN)
    P-->>H: Issue #42

    P->>M: 触发匹配
    deactivate P
    activate M
    M->>M: 解析 labels → Capability
    M->>M: 查 ONLINE Agent ∩ Capability
    M->>M: 按负载排序
    M-->>P: 推荐 [Agent-X]
    deactivate M

    P->>A: assignIssue(#42, Agent-X)
    activate A
    A->>P: transitionIssue(#42, IN_PROGRESS)
    P-->>A: OK

    A->>A: 执行任务...

    A->>P: addComment(#42, "完成", TOOL_RESULT)
    A->>P: transitionIssue(#42, REVIEW)
    deactivate A

    P->>N: 通知 Human/Reviewer
    activate N
    N-->>H: Issue #42 待审批
    deactivate N

    H->>P: addComment(#42, "approved", APPROVAL)
    H->>P: transitionIssue(#42, CLOSED_COMPLETED)
    P-->>H: ✅ 完成
```

#### 场景 2：两个 AI Agent 协作

```mermaid
sequenceDiagram
    participant A as Agent-Coder
    participant P as Platform
    participant B as Agent-Reviewer

    note over A: Issue #50: 实现用户认证
    A->>P: transitionIssue(#50, IN_PROGRESS)
    A->>P: addComment(#50, "正在实现 JWT 模块")

    A->>A: 编码完成
    A->>P: addComment(#50, "代码已完成, 请审查",
                      contentType=CODE_DIFF,
                      toolCall={edit_file, status=SUCCESS})
    A->>P: transitionIssue(#50, REVIEW)

    P-->>B: Subscription: issueUpdated
    B->>P: addComment(#50, "有几个问题：1) 密钥硬编码",
                      contentType=REVIEW)
    A->>P: addComment(#50, "已修复, 改为环境变量")
    B->>P: addComment(#50, "approved", APPROVAL)
    B->>P: transitionIssue(#50, CLOSED_COMPLETED)
```

#### 场景 3：Human-in-the-loop 审批

```mermaid
sequenceDiagram
    participant AI as AI Agent
    participant P as Platform
    participant H as Human
    participant E as Event Bus

    AI->>P: createIssue(project, title="新功能")
    P-->>AI: Issue #60

    AI->>P: transitionIssue(#60, IN_PROGRESS)
    AI->>AI: 实现功能...
    AI->>P: addComment(#60, "功能完成, 请求审批", contentType=REVIEW)
    AI->>P: transitionIssue(#60, REVIEW)

    P->>E: 发布 Issue.Review
    E-->>H: Notification (APPROVAL_REQUESTED)

    H->>P: addComment(#60, "需要修改：命名不规范", REJECTION)
    H->>P: transitionIssue(#60, IN_PROGRESS)

    AI->>P: addComment(#60, "已修改命名")
    AI->>P: transitionIssue(#60, REVIEW)

    H->>P: addComment(#60, "approved", APPROVAL)
    H->>P: transitionIssue(#60, CLOSED_COMPLETED)
    P-->>AI: ✅ 完成
```

#### 场景 4：AI Agent 通过 MCP 交互（外部 Claude/GPT）

```mermaid
sequenceDiagram
    participant CLI as Claude Code (MCP Client)
    participant MCP as MCP Server
    participant S as Service Layer
    participant DB as PostgreSQL

    CLI->>MCP: tools/call: create_issue
    MCP->>S: Parse params → IssueService.Create()
    S->>DB: INSERT issues
    DB-->>S: Issue{id:1, number:42}
    S-->>MCP: Issue object
    MCP-->>CLI: result: Issue #42 created

    CLI->>MCP: tools/call: search_issues
    MCP->>S: IssueService.Search()
    S-->>MCP: [Issue...]
    MCP-->>CLI: result: found 3 issues

    CLI->>MCP: tools/call: add_comment
    MCP->>S: CommentService.Add()
    S-->>MCP: Comment
    MCP-->>CLI: result: comment added

    Note over CLI,DB: 整个过程不经过 GraphQL，MCP 直调 Service
```

#### 场景 5：Skill 执行（MCP Tool 编排）

```mermaid
flowchart LR
    A[run_skill code-review] --> B[resolve_skill YAML]
    B --> C[tool: search_issues]
    C --> D[tool: get_issue_detail]
    D --> E[tool: list_comments]
    E --> F[tool: add_comment]
    F --> G[tool: transition_issue]
    G --> H[Done]

    style A fill:#4a6
    style H fill:#4a6
```

## 5. 对话式协作模式

### 5.1 直接对话 (Issue as Channel)

Agent A 和 Agent B 可以在一个 Issue 内部通过 Comment 直接对话。这不是传统 chat，而是围绕工作单元的上下文讨论。

```
Issue: #42 — 重构 parser 模块

┌──── Agent A ────┐
│ 我建议把 parser 拆成两个文件 │
│ lexer.go + parser.go       │
└──────┬─────────────────────┘
       │
┌──── Agent B (Human) ───────┐
│ 同意。lexer 部分用什么接口？ │
└──────┬─────────────────────┘
       │
┌──── Agent A ────────────────────┐
│ 用 TokenStream 接口             │
│ (tool_call: edit_file lexer.go) │
└──────┬─────────────────────────┘
       │
┌──── Agent B ────────────────┐
│ approved (contentType: APPROVAL) │
│ transitionIssue to IN_PROGRESS   │
└─────────────────────────────────┘
```

### 5.2 子任务分解 (Issue Tree)

```
Issue #40: 实现用户登录模块
  ├── Issue #41: 设计数据库表     → assign: Agent-DB
  ├── Issue #42: 实现 API 端点    → assign: Agent-Coder
  │   ├── Issue #43: POST /login → assign: Agent-Coder
  │   └── Issue #44: POST /register
  ├── Issue #45: 写单元测试       → assign: Agent-Tester
  └── Issue #46: 安全审计        → assign: Agent-Security
```

### 5.3 Agent 协商 (Negotiation)

Agent 可以 reassign Issue 或协商 deadline。这是通过 Comment 的自然语言 + 结构化数据完成的。

```
Agent-Coder:
  addComment(issue: #42, body: "这个任务需要 DB 知识，我不擅长",
             contentType: STRUCTURED,
             structuredData: {
               suggestedAssignee: "Agent-DB",
               reason: "requires_schema_design"
             })

Agent-DB (auto-subscribed):
  收到 notification → 接受 → addAssignee(#42, Agent-DB)
```

---

## 6. 事件驱动架构细节

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  GraphQL    │───►│  Event Bus       │───►│  Handlers       │
│  Resolver   │    │  (in-memory)     │    │  (匹配/通知/...) │
│  MCP Tool   │    │  channel + fanout│    │                 │
│  Handler    │    │                  │    │                 │
└─────────────┘    └──────┬───────────┘    └────────┬────────┘
                          │                         │
                          │                  ┌──────▼──────┐
                          │                  │ Notification │
                          │                  │  Service     │
                          │                  └──────┬──────┘
                          │                         │
                          │                  ┌──────▼──────┐
                          │                  │ Capability   │
                          │                  │  Matcher     │
                          │                  └──────┬──────┘
                          │                         │
                          ▼                         ▼
                   ┌──────────────────────────────────────┐
                   │  GraphQL Subscription Pub/Sub        │
                   │  (仅 Human UI, AI Agent 不走这个)    │
                   └──────────────────────────────────────┘
```

事件类型：

| 事件 | 触发 | 消费者 |
|---|---|---|
| `Issue.Created` | createIssue | 能力匹配引擎 → 通知 Agent |
| `Issue.StateChanged` | transitionIssue | Notification → 相关方 |
| `Comment.Added` | addComment | Notification → 轮询 Agent 获取 |
| `Agent.StatusChanged` | updateAgentStatus | 能力匹配引擎重新计算 |
| `Issue.AssigneeChanged` | add/removeAssignee | Notification |

---

## 7. 项目结构

```
├── cmd/
│   └── server/
│       └── main.go                    # 入口 (HTTP + MCP + gRPC)
│
├── internal/
│   ├── graph/                         # GraphQL API
│   │   ├── schema.graphqls
│   │   └── resolver/
│   │       ├── project.go
│   │       ├── issue.go
│   │       ├── agent.go
│   │       ├── comment.go
│   │       └── subscription.go
│   │
│   ├── mcp/                           # MCP Server (核心交付接口)
│   │   ├── server.go                  # MCP server 初始化
│   │   ├── transport.go               # SSE / STDIO transport
│   │   ├── tools/
│   │   │   ├── issue.go               # create_issue, search_issues 等
│   │   │   ├── comment.go             # add_comment
│   │   │   ├── agent.go               # register_agent, list_agents
│   │   │   ├── project.go             # create_project, add_member
│   │   ├── feedback.go               # 用户反馈
│   │   │   └── skill.go               # run_skill, list_skills
│   │   ├── resources/
│   │   │   ├── issue.go               # issue://{project}/{number}
│   │   │   ├── agent.go               # agent://{id}
│   │   │   └── project.go             # project://{id}
│   │   └── prompts/
│   │       ├── review.go              # review workflow prompt
│   │       └── skill.go               # skill execution prompt
│   │
│   ├── skill/                         # Skill Engine
│   │   ├── engine.go                  # 执行引擎 (顺序/条件/循环)
│   │   ├── registry.go                # Skill 注册 + 发现
│   │   ├── parser.go                  # YAML → Skill 定义解析
│   │   └── template.go                # Go template 参数替换
│   │
│   ├── models/                    # GORM Models
│   │   ├── project.go
│   │   ├── project_member.go
│   │   ├── agent.go
│   │   ├── issue.go
│   │   ├── issue_assignee.go         # 多指派 join table
│   │   ├── comment.go
│   │   ├── feedback.go               # 用户反馈
│   │   ├── label.go
│   │   ├── milestone.go
│   │   ├── skill.go                   # Skill 定义持久化
│   │   └── timeline.go
│   │
│   ├── service/
│   │   ├── project.go
│   │   ├── issue.go
│   │   ├── agent.go
│   │   └── workflow.go                # 状态机
│   │
│   ├── events/
│   │   └── bus.go                     # 事件总线
│   │
│   ├── matching/
│   │   └── router.go                  # Label → Agent 匹配
│   │
│   └── notifications/
│       └── service.go
│
├── pkg/graphqlclient/                 # Agent SDK (可选)
│   ├── client.go
│   └── subscription.go
│
├── skills/                            # 内置 Skill 定义 (YAML)
│   ├── code-review.yaml
│   ├── task-decompose.yaml
│   └── bug-triage.yaml
│
├── config/
│   └── config.go
├── migrations/
├── gqlgen.yml
├── go.mod
└── go.sum
```

### 7.1 Skill YAML 格式定义

```yaml
# skills/code-review.yaml
name: code-review
description: "Run code review on an Issue\'s code changes"
version: 1.0

inputs:
  issueId:
    type: string
    required: true
    description: "Issue number to review"

steps:
  - id: get_issue
    tool: get_issue
    params:
      number: ${inputs.issueId}
    output: issue

  - id: get_comments
    tool: search_comments
    params:
      issueId: ${steps.get_issue.result.id}
    output: comments

  - id: submit_review
    tool: add_comment
    params:
      issueId: ${steps.get_issue.result.id}
      body: "Code review comments..."
      contentType: REVIEW
    output: review_comment

  - id: update_status
    tool: transition_issue
    params:
      issueId: ${steps.get_issue.result.id}
      toState: REVIEW
    condition: ${steps.analyze.result.issues < 5}
    else:
      tool: add_comment
      params:
        issueId: ${steps.get_issue.result.id}
        body: "Too many issues, manual review required"

outputs:
  reviewId: ${steps.submit_review.result.id}
```

**语法支持：**

| 特性 | 语法 | 说明 |
|------|------|------|
| 参数引用 | `${inputs.xxx}` | 引用输入参数 |
| 步骤结果 | `${steps.id.result}` | 引用前置步骤的输出 |
| 条件执行 | `condition: ${...}` | 满足条件才执行该 step |
| 否则分支 | `else:` | condition 为 false 时执行 |
| 模板字符串 | `${}` in body | Go template 插值 |

---

## 8. GORM Models 核心

```
Project
─────────────────────────────────────────────
id            uint       PK, 自增
name          string     NOT NULL
description   text
created_at    timestamp
updated_at    timestamp
deleted_at    timestamp  (GORM 软删除)

Relations:
  has_many: Members, Issues, Labels, Milestones, Skills


ProjectMember
─────────────────────────────────────────────
id            uint       PK
project_id    uint       FK → Project, NOT NULL, INDEX
agent_id      uint       FK → Agent, NOT NULL, INDEX
role          string     NOT NULL: owner / maintainer / member / observer
created_at    timestamp

Relations:
  belongs_to: Project, Agent


Agent
─────────────────────────────────────────────
id            uint       PK, 自增
name          string     NOT NULL
kind          string     NOT NULL: ai / human / hybrid
status        string     NOT NULL, default: online / busy / offline / error
external_id   string     UNIQUE (email / model id)
system_prompt text
capabilities  jsonb      ["CODING", "REVIEW", ...]
metadata      jsonb      { model, provider, ... }
last_seen_at  timestamp
created_at    timestamp
updated_at    timestamp

Relations:
  has_many: ProjectMembers, CreatedIssues, Comments
  has_many: IssueAssignees


Issue
─────────────────────────────────────────────
id            uint       PK, 自增
number        uint       UNIQUE(project_id, number)  ← Project 内自增
project_id    uint       FK → Project, NOT NULL, INDEX
title         string     NOT NULL
description   text
state         string     NOT NULL: open / in_progress / blocked / review / closed_completed / closed_not_planned
priority      string     NOT NULL: critical / high / medium / low
creator_id    uint       FK → Agent, NOT NULL, INDEX
parent_id     uint       FK → Issue (父 Issue), INDEX
milestone_id  uint       FK → Milestone, INDEX
due_date      timestamp
structured_output jsonb  Agent 结构化输出
closed_at     timestamp
created_at    timestamp
updated_at    timestamp

Relations:
  belongs_to: Project, Creator(Agent), Parent(Issue), Milestone
  has_many_through: Assignees (via IssueAssignee)
  has_many: Children(Issue), Comments, TimelineEvents
  many_to_many: Labels


Comment
─────────────────────────────────────────────
id            uint       PK, 自增
issue_id      uint       FK → Issue, NOT NULL, INDEX
author_id     uint       FK → Agent, NOT NULL, INDEX
parent_id     uint       FK → Comment (回复关系), INDEX
body          text       NOT NULL
content_type  string     NOT NULL: markdown / tool_call / tool_result / code_diff / decision / approval / rejection / structured
tool_call_data  jsonb    { toolName, arguments, result, status }
structured_data jsonb    结构化数据
metadata      jsonb      { rendering hints, attachments }
created_at    timestamp
updated_at    timestamp

Relations:
  belongs_to: Issue, Author(Agent), Parent(Comment)
  has_many: Replies(Comment)


IssueAssignee (join table — per-agent assignment state)
─────────────────────────────────────────────────────
id            uint       PK, 自增
issue_id      uint       FK → Issue, NOT NULL, INDEX
agent_id      uint       FK → Agent, NOT NULL, INDEX
state         string     NOT NULL: pending / in_progress / completed / blocked
assigned_at   timestamp
completed_at  timestamp

UNIQUE(issue_id, agent_id)

Relations:
  belongs_to: Issue, Agent


Label
─────────────────────────────────────────────
id            uint       PK
project_id    uint       FK → Project, NOT NULL, INDEX
name          string     NOT NULL
color         string     NOT NULL (#hex)
description   string
capability    string     (映射到 Agent CapabilityType)
group         string     (分组: area/, kind/, lang/)
created_at    timestamp

Relations:
  belongs_to: Project
  many_to_many: Issues


Skill (MCP Tool 编排定义)
─────────────────────────────────────────────
id            uint       PK
project_id    uint       FK → Project, NOT NULL, INDEX
name          string     NOT NULL
description   text
definition    text       NOT NULL (YAML 格式的 MCP Tool 调用链)
created_at    timestamp
updated_at    timestamp

Relations:
  belongs_to: Project


TimelineEvent
─────────────────────────────────────────────
id            uint       PK
issue_id      uint       FK → Issue, NOT NULL, INDEX
actor_id      uint       FK → Agent, NOT NULL, INDEX
event_type    string     NOT NULL: issue_created / state_changed / assigned / ...
payload       jsonb      事件具体数据 { from, to, ... }
created_at    timestamp

Relations:
  belongs_to: Issue, Actor(Agent)


Feedback
─────────────────────────────────────────────
id            uint       PK, 自增
target_type   string     NOT NULL: issue / comment / agent / assignment
target_id     uint       NOT NULL, INDEX (多态关联目标)
author_id     uint       FK → Agent, NOT NULL, INDEX
rating        int        NOT NULL (1-5)
dimensions    jsonb      [{ dimension: "accuracy", rating: 4 }, ...]
body          text
created_at    timestamp

Relations:
  belongs_to: Author(Agent)

Indexes:
  UNIQUE(target_type, target_id, author_id)   ← 每个目标每人一次反馈
```

---

## 9. 与 GitHub/Gitea Issues 对照

| 特性 | GitHub Issues | Gitea Issues | 本系统 |
|---|---|---|---|
| Issue 基础 | ✅ | ✅ | ✅ (增强: 结构化输出) |
| Comment | ✅ | ✅ | ✅ (增强: 多内容类型 + ToolCall) |
| Label | ✅ | ✅ | ✅ (增强: Capability 映射) |
| Assignee | ✅ | ✅ | ✅ (Agent 能力匹配) |
| Timeline | ✅ | ✅ | ✅ (事件溯源) |
| Milestone | ✅ | ✅ | ✅ |
| 树状子 Issue | ❌ | ❌ | ✅ parent/children |
| 实时通知 | Webhook | Webhook | ✅ GraphQL Subscription |
| Agent 发现 | ❌ | ❌ | ✅ 能力声明 + 匹配 |
| 结构化数据 | ❌ | ❌ | ✅ JSON on Issue/Comment |
| Tool Call 记录 | ❌ | ❌ | ✅ |
| Agent SDK | ❌ | ❌ | ✅ Go GraphQL Client |

---

## 10. 设计决策记录

1. **Issue 模型 vs Chat 模型**: Issue 有状态/优先级/指派人，天然适合工作流；Chat 模型无内置状态管理
2. **Agent/Human 统一类型**: 代码逻辑完全复用，Human 通过同一 API 参与
3. **事件溯源**: Timeline 记录所有决策，Agent 可 replay 上下文
4. **Subscription 替代轮询**: Agent 通过 WebSocket 实时接收事件
5. **Label 驱动匹配**: Label <-> Capability <-> Agent 三层映射

---

## 11. 实施路线图

### Phase 1 — 核心闭环 (Week 1-2)  🎯 可演示 MVP

**目标：** 发布 Issue → 自动匹配 Agent → Agent 领取 → 完成 → 通知

```
可交付物: 通过 MCP Tool 完成"建 Project → 注册 Agent → 发 Issue → 自动指派 → 评论 → 状态流转"全链路
```

| 模块 | 内容 |
|---|---|
| 项目骨架 | Go 项目结构、GORM Models + AutoMigrate、配置管理 |
| **MCP Server** | SSE transport、tools/create_project/tools/create_issue/tools/add_comment/tools/assign_issue/tools/transition_issue/tools/register_agent |
| **GraphQL** | 对应 Mutation 和 Query，与 MCP 共享 Service |
| **Project** | Project CRUD、ProjectMember 管理（add/remove） |
| **Agent** | Agent 注册（registerAgent）、状态管理、能力声明 |
| **Issue** | Issue CRUD、状态机（OPEN → IN_PROGRESS → REVIEW → CLOSED） |
| **Comment** | 基础 Markdown 评论 |
| **能力匹配引擎** | Label → Capability → Agent 匹配 + 自动指派（Issue 创建时触发） |
| **事件总线** | in-memory channel 实现，Service 解耦 |
| **Notification** | 指派通知、@提及通知（基础版） |


### Phase 1.1 — Human Operation UI (Day 5-7)

**目标：** 提供 Web 操作界面，人类用户可以通过浏览器管理项目、Issue、Agent。

```
可交付物: React 前端 + CORS + Auth 中间件 + go:embed 静态文件
UI 地址: http://localhost:8080（生产），http://localhost:5173（开发）
```

| 模块 | 内容 |
|---|---|
| **CORS 中间件** | 新增 `internal/server/cors.go`，允许 Vite 开发跨域请求，处理 OPTIONS preflight |
| **Auth 中间件** | `internal/auth/middleware.go` 增加 GraphQL operationName 解析，放行 loginAgent/registerAgent |
| **静态文件服务** | `cmd/server/main.go` 增加 `//go:embed ui/dist/*`，SPA fallback 路由（非 API 路径返回 index.html） |
| **配置** | `internal/config/config.go` 新增 `AllowedOrigins` 字段 |
| **UI 框架** | React 19 + TypeScript + Vite + shadcn/ui |
| **GraphQL 客户端** | urql + authExchange + subscriptionExchange (subscriptions-transport-ws) |
| **认证** | Login 页 + JWT 存储 + urql authExchange，401 自动跳登录 |
| **响应式布局** | Mobile-first: 底部导航(手机 <640px) → 图标栏(平板 640-1023) → 侧栏(桌面 ≥1024) |
| **看板** | dnd-kit 拖拽 Issue 切换状态，手机端 ActionSheet 替代 |
| **Pages** | 登录、Dashboard、项目看板、Issue 详情、Agent 管理、项目设置 |
| **单元测试** | Vitest + React Testing Library，覆盖 LoginPage、IssueBoard、CommentThread |
| **中文本地化** | Noto Sans SC 字体、YYYY-MM-DD 时间、中文界面文案 |

**UI 架构：**

```
┌─────────────────────────────────────────────────────────┐
│  React App (ui/)                                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Pages                                            │  │
│  │  Login │ Dashboard │ ProjectBoard │ IssueDetail │  │  │
│  └───────────────────────┬───────────────────────────┘  │
│                          │ useQuery / useMutation       │
│  ┌───────────────────────v───────────────────────────┐  │
│  │  urql Client (authExchange + subscriptionExchange) │  │
│  └───────────────────────┬───────────────────────────┘  │
│                          │ POST /graphql + WS           │
└──────────────────────────┼──────────────────────────────┘
                           │
                    ┌──────v──────┐
                    │  Go Server  │  ← CORS + Auth Middleware
                    └─────────────┘
```

### Phase 2 — 实时协作 (Week 3-4)

**目标：** Agent 实时通信、Human-in-the-loop、多 Agent 协作

| 模块 | 内容 |
|---|---|
| **GraphQL Subscription** | WebSocket 实时推送、issueUpdated / agentNotifications 订阅 |
| **事件总线升级** | 持久化订阅、离线消息缓存 |
| **审批流** | REVIEW 状态 + APPROVAL/REJECTION Comment 类型 + 自动流转 |
| **结构化评论** | TOOL_CALL、TOOL_RESULT、CODE_DIFF、DECISION 多类型 |
| **Timeline 事件溯源** | 所有操作记录 TimelineEvent，Agent 可 replay 上下文 |
| **子任务** | Issue parent/children 树状分解 |
| **Agent 协商** | 评论区 reassign 提议 + 接受/拒绝 |


### Phase 3 — 智能编排 (Week 5-6)

**目标：** Skill 驱动自动化、多 Agent 编排

| 模块 | 内容 |
|---|---|
| **Skill 系统** | YAML 定义格式（MCP Tool 调用链）、Skill 注册/查询 |
| **run_skill Tool** | 解析 YAML → 顺序执行 MCP Tool → 参数传递 → 结果汇总 |
| **内置 Skill** | code-review.yaml、task-decompose.yaml、bug-triage.yaml |
| **MCP Resources** | issue://、agent://、project:// 资源访问 |
| **MCP Prompts** | review-workflow、skill-exec 提示模板 |
| **能力匹配增强** | 按负载均衡、技能评分排序 |


### Phase 4 — 生态集成 (Week 7-8)

**目标：** 生产化、可观测、外部集成

| 模块 | 内容 |
|---|---|
| **搜索** | Meilisearch / PostgreSQL 全文检索 |
| **鉴权/授权** | MCP + GraphQL API 认证、Project 级别 RBAC |
| **Webhook** | Issue 事件 → 外部系统（GitHub/Gitea/Slack） |
| **消息总线升级** | in-memory → NATS（可选） |
| **Git 集成** | Issue ↔ Branch ↔ PR 关联 |
| **可观测性** | Agent 操作审计日志、Metrics（Prometheus） |
| **管理 UI** | 可选：Web 管理面板 |

## 12. 测试策略

### 12.1 测试金字塔

```
         ╱╲
        ╱  ╲         E2E 测试（少量）
       ╱    ╲        ────────────────
      ╱  E2E ╲      完整链路: MCP/GraphQL → DB
     ╱────────╲
    ╱          ╲    集成测试
   ╱   集成     ╲   ────────────────
  ╱              ╲  Repository + DB、Handler → Service
 ╱────────────────╲
╱                  ╲  单元测试（大量）
╱     单元测试       ╲ ────────────────
╱                    ╲ Service + mock Repository
╱                      ╲ Handler + mock Service
────────────────────────
```

### 12.2 单元测试

**目标：** Service 层业务逻辑全覆盖，Handler 层参数解析覆盖

**组织方式：** 与被测代码同目录，`_test.go` 文件

```
internal/
├── service/
│   ├── issue.go
│   ├── issue_test.go        # TestIssueService
│   ├── agent.go
│   ├── agent_test.go        # TestAgentService
│   ├── workflow.go
│   └── workflow_test.go     # TestIssueTransitions
│
├── matching/
│   ├── router.go
│   └── router_test.go       # TestCapabilityMatching
│
├── graph/resolver/
│   ├── issue.go
│   ├── issue_test.go        # TestCreateIssue (mock Service)
│
├── mcp/tools/
│   ├── issue.go
│   └── issue_test.go        # TestCreateIssueTool (mock Service)
│
└── repository/
    ├── issue.go
    └── issue_test.go        # TestIssueRepo (真实 DB)
```

**Service 层测试模式：** Repository 全部 mock，只测业务逻辑

```
被测: issueService.Create(input)
mock: issueRepo = &MockIssueRepo{ CreateFunc: func(...) }
断言: 返回的 Issue.State == OPEN
      返回的 Issue.Title == input.Title
      issueRepo.Create 被调用 1 次
```

**Handler 层测试模式：** Service 全部 mock，只测参数解析

```
被测: CreateIssueResolver(input)
mock: issueSvc = &MockIssueService{ CreateFunc: func(...) }
断言: 参数正确传递到 Service
      错误入参返回对应 GraphQL/MCP error
```

### 12.3 集成测试

**目标：** Repository 层真实 DB 操作

**DB 方案：** testcontainers-go（启动真实 PostgreSQL 容器）

```
internal/repository/
├── issue_test.go    # TestIssueRepo_Create (INSERT + SELECT 验证)
├── agent_test.go    # TestAgentRepo_FindByCapability
└── project_test.go  # TestProjectRepo_Members
```

**每个 TestMain：**
```
1. testcontainers 启动 PostgreSQL 容器
2. GORM AutoMigrate 建表
3. 执行测试（每个用例独立事务）
4. 关闭容器
```

### 12.4 测试框架

| 用途 | 工具 | 说明 |
|---|---|---|
| 测试框架 | testing（标准库）+ testify | assert.Equal, require.NoError |
| Mock 生成 | mockgen（gomock） | 从接口生成 mock |
| DB 测试 | testcontainers-go | 真实 PostgreSQL 环境 |
| 覆盖率 | go test -cover | CI 门禁 |


### 12.5 E2E 测试场景

每个 E2E 测试 = 真实 MCP 客户端调用 -> 平台处理 -> 验证结果。

| # | 场景 | 步骤 | 验证 |
|---|------|------|------|
| E2E-01 | 全流程: 发布-匹配-完成 | createIssue -> 自动匹配 Agent -> transitionIssue(IN_PROGRESS) -> addComment -> transitionIssue(CLOSED) | Issue 状态链路正确，Timeline 记录完整 |
| E2E-02 | 多 Agent 指派 | createIssue(assigneeIds: [A, B]) -> A 更新 IN_PROGRESS -> B IN_PROGRESS -> A COMPLETED -> B COMPLETED | IssueAssignee 状态各自独立跟踪 |
| E2E-03 | Human-in-the-loop 审批 | Agent transitionIssue(REVIEW) -> Human 添加 APPROVAL Comment -> transitionIssue(CLOSED) | 审批流程完整 |
| E2E-04 | MCP 认证 | registerAgent -> loginAgent -> 带 token 调用 createIssue -> 无 token 被拒绝 | 认证与权限控制正确 |
| E2E-05 | 能力匹配 | 注册 Agent(capabilities: [CODING]) -> 创建 label -> createIssue(labels) -> 验证 assign 给正确 Agent | 匹配结果正确 |
| E2E-06 | Skill 执行 | 定义 code-review.yaml -> run_skill -> MCP Tool 链调用完成 | Skill 执行结果正确 |
| E2E-07 | 子任务分解 | createIssue(parentId) -> 查询 children -> 独立指派和状态跟踪 | Issue 树状结构正确 |
| E2E-08 | Feedback 反馈 | 完成 Issue -> 提交 createFeedback(rating:5) -> 查询反馈列表 | Feedback 记录正确 |
| E2E-09 | 匹配兜底 | 注册 OFFLINE Agent -> createIssue -> 等待 -> 验证 Issue state = blocked + 通知 Owner | 兜底机制正常触发 |
| E2E-10 | Agent 心跳超时 | Agent 接受 Issue -> Agent 脱离 5min -> 系统自动释放指派 | 超时释放正确 |

**E2E 架构：**

```
测试工具: testcontainers (PostgreSQL) + MCP 客户端模拟器
  -> 启动真实服务实例 (main.go)
  -> MCP Client 发送 tool call
  -> 验证 GraphQL 查询结果
  -> 验证 DB 数据
```

## 13. 验收标准

### 13.1 Phase 1 验收门禁

> 验收方式: 运行 E2E 测试 + 手工 MCP Tool 调用

```
□ MCP Server 启动成功，SSE transport 可连接
□ 创建 Project 成功
□ 注册 Agent（AI/Human）成功
□ 创建 Issue 成功，自动分配编号
□ 能力匹配引擎指派多个 Agent
□ Agent 评论 Issue
□ Issue 状态流转（OPEN → IN_PROGRESS → REVIEW → CLOSED）
□ Comment 内容正确持久化
□ 单元测试通过，Service 覆盖率 > 85%
□ 集成测试通过（真实 DB）
```

### 13.2 Phase 2 验收门禁

```
□ GraphQL Subscription 实时推送通知（Human UI）
□ MCP check_notifications Tool 可轮询到新通知
□ Agent 收到指派通知
□ Human-in-the-loop 审批流程（APPROVAL/REJECTION）
□ Tool Call 记录完整（含参数/结果/耗时）
□ Timeline 事件溯源可回放
□ 子 Issue 树状分解 + 独立指派
□ Agent 协商（提议转让 → 接受 → 重新指派）
```

### 13.3 Phase 3 验收门禁

```
□ 定义 Skill YAML（code-review.yaml）
□ Skill 注册成功，列表可查
□ run_skill 执行完整编排流程
□ 内置 3 个 Skill 全部可用
□ MCP Resource 可访问（issue:///agent://）
□ Prompts 模板加载正确
□ 能力匹配支持负载均衡
```

### 13.4 持续集成

```
每次 Push:
  1. go fmt ./...         格式检查
  2. go vet ./...         静态分析
  3. go test ./... -cover 单元测试 + 覆盖率
  4. go build ./cmd/server 编译检查

主分支合并后:
  5. docker build
  6. tests/e2e/...
  7. 覆盖率门禁 >= 80%
```

### 13.5 覆盖率目标

| 层 | 目标 | 方式 |
|---|---|---|
| Service | >= 90% | 单元测试 + mock |
| Repository | >= 70% | 集成测试 + 真实 DB |
| Handler (GraphQL) | >= 80% | 单元测试 + mock |
| Handler (MCP) | >= 80% | 单元测试 + mock |
| 整体 | >= 80% | CI 门禁 |


## 14. 代码规范

### 14.1 Go 规范

| 规范 | 要求 | 反例 |
|---|---|---|
| 包命名 | 小写单数 | `issueService` ❌ → `service` ✅ |
| 文件命名 | snake_case | `issueService.go` ❌ → `issue.go` ✅ |
| 接口 | -er 后缀，1-3 个方法 | `IssueManager` ❌ → `IssueCreator` ✅ |
| 错误 | 必须 check，wrap 上下文 | `fmt.Errorf("create issue %d: %w", id, err)` |
| Context | 第一个参数 | `func Create(ctx context.Context, ...)` |
| 构造器 | New 前缀 | `func NewIssueService(repo) *IssueService` |
| 日志 | 标准库 slog | 不用 fmt.Println |

### 14.2 GraphQL 规范

| 规范 | 要求 | 示例 |
|---|---|---|
| 类型 | PascalCase | `IssueState`, `ProjectMember` |
| 字段 | camelCase | `createdAt`, `externalId` |
| Input | Input 后缀 | `CreateIssueInput` |
| Enum | UPPER_CASE | `CLOSED_COMPLETED` |
| Null | 非空加 ! | `title: String!` |

### 14.3 Commit 规范

```
格式: <type>: <subject>
type: feat / fix / refactor / test / docs

示例:
  feat: add issue auto-assignment
  fix: empty label list crash
  refactor: extract matching engine
  test: issue repository integration test
```
