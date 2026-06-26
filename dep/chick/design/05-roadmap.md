# 实施路线图

## Phase 1 — 核心闭环 (已完成)

**目标：** 发布 Issue → 自动匹配 Agent → Agent 领取 → 完成 → 通知

已交付：
- Go 项目骨架、GORM Models、配置管理
- MCP Server (SSE) + 12 个 Tool
- GraphQL API (13 Query + 26 Mutation + 3 Subscription)
- Project CRUD + 成员管理
- Agent 注册/状态/能力声明
- Issue CRUD + 状态机 (6 states)
- Comment + Timeline 事件溯源
- 能力匹配引擎 (Label → Capability → Agent)
- 事件总线 (in-memory)
- 通知服务 + WebSocket Subscription

## Phase 1.1 — Human Operation UI (进行中)

**目标：** 提供 Web 操作界面，人类用户通过浏览器管理项目、Issue、Agent。

| 模块 | 内容 |
|------|------|
| CORS 中间件 | `internal/server/cors.go`，Vite 开发跨域 + OPTIONS preflight |
| Auth 中间件 | GraphQL operationName 放行 loginAgent/registerAgent |
| 静态文件服务 | `//go:embed ui/dist/*` + SPA fallback |
| UI 框架 | React 19 + TypeScript + Vite + shadcn/ui |
| GraphQL 客户端 | urql + authExchange + subscriptionExchange |
| 认证 | Login 页 + JWT 存储 + 401 自动跳登录 |
| 响应式布局 | Mobile-first: 底部导航 → 图标栏 → 侧栏 |
| 看板 | dnd-kit 拖拽，手机端 ActionSheet 降级 |
| Pages | 登录、Dashboard、项目看板、Issue 详情、Agent 管理、项目设置 |
| 中文本地化 | 系统字体优先、YYYY-MM-DD 时间、中文界面 |

**实现顺序：**
1. Backend: CORS + Static Serving + Auth middleware
2. Frontend Scaffold (Vite + shadcn + urql + router)
3. Auth (LoginPage + AuthProvider)
4. Layout (Sidebar + TopBar + MobileNav)
5. Dashboard (Project list + stats + agent status)
6. Project Detail (IssueBoard + dnd-kit)
7. Issue Detail (Comments + Timeline + Transitions)
8. Agent Management (List + filters)
9. Project Settings (Labels + Milestones + Members)
10. Dark Mode

## Phase 1.2 — 自动化测试

| 测试层 | 工具 | 范围 |
|--------|------|------|
| 单元测试 | Vitest + React Testing Library | 组件渲染、Hook 逻辑、表单验证 |
| 集成测试 | Vitest + MSW | GraphQL 组件交互 |
| E2E 测试 | Playwright | 登录 → 创建 Issue → 拖拽 → 评论全流程 |

## Phase 2 — 实时协作

GraphQL Subscription 实时推送、审批流、结构化评论、子任务、Agent 协商。

## Phase 3 — 智能编排

MCP Resources、Prompts、匹配增强。

## Phase 4 — 生态集成

搜索、Webhook、Git 集成、NATS、可观测性、管理面板。

---

## 测试策略

### 单元测试（Go 后端）

Service 层全覆盖，Repository 全部 mock，Handler 层参数解析覆盖。

```
internal/service/issue_test.go    → Service 业务逻辑
internal/service/workflow_test.go → 状态机转换
internal/matching/router_test.go  → 能力匹配
internal/mcp/server_test.go       → MCP 工具调用
internal/graph/resolver/          → GraphQL resolver
internal/repository/              → 真实 DB 集成测试
```

### 覆盖率目标

| 层 | 目标 | 方式 |
|---|---|---|
| Service | ≥ 90% | 单元测试 + mock |
| Repository | ≥ 70% | 集成测试 + 真实 DB |
| Handler | ≥ 80% | 单元测试 + mock |
| 整体 | ≥ 80% | CI 门禁 |

---

## 代码规范

### Go

| 规范 | 要求 |
|------|------|
| 包命名 | 小写单数 |
| 文件命名 | snake_case |
| 接口 | -er 后缀，1-3 方法 |
| 错误 | 必须 check，wrap 上下文 |
| Context | 第一个参数 |
| 构造器 | New 前缀 |
| 日志 | 标准库 slog |

### Commit

```
格式: <type>: <subject>
type: feat / fix / refactor / test / docs
```

### CI

```
每次 Push:
  1. go fmt ./...
  2. go vet ./...
  3. go test ./... -cover
  4. go build ./cmd/server
```

---

## 与 GitHub Issues 对照

| 特性 | GitHub | Gitea | Chick |
|------|--------|-------|-------|
| Issue 基础 | ✅ | ✅ | ✅ + 结构化输出 |
| Comment | ✅ | ✅ | ✅ + 多内容类型 |
| Label | ✅ | ✅ | ✅ + Capability 映射 |
| Assignee | ✅ | ✅ | ✅ + 能力匹配 |
| Timeline | ✅ | ✅ | ✅ + 事件溯源 |
| 树状子 Issue | ❌ | ❌ | ✅ |
| 实时通知 | Webhook | Webhook | ✅ GraphQL Subscription |
| Agent 发现 | ❌ | ❌ | ✅ 能力声明 + 匹配 |
| Tool Call | ❌ | ❌ | ✅ |
