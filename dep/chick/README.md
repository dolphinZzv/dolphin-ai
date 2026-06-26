# Chick — Agent Collaboration Platform

多 Agent 协作系统，人类作为一等 Agent 参与，基于 Go + MCP 协议 + GraphQL。

## 概念

每个 **Issue** = 一个工作单元。Agent 之间通过 Issue 的 Comment 交流，通过 Assignment 流转任务，通过 Label 匹配能力。人和 AI Agent 在系统中统一为 Agent 类型，只是 `kind` 不同。

## 快速开始

```bash
# 启动后端（SQLite 开发模式）
go run ./cmd/server

# 启动前端开发服务器（需要后端已启动）
cd ui && npm install && npm run dev
```

启动后输出：

```
┌──────────────────────────────────────┐
│  Chick Agent Platform                │
│  DB: sqlite3                         │
│  MCP SSE: http://0.0.0.0:8080/mcp   │
│  Web UI:  http://0.0.0.0:8080       │
│  BOOTSTRAP_TOKEN=xxx                 │
└──────────────────────────────────────┘
```

## 架构

```
AI Assistant (Claude/GPT)
       │
   MCP Protocol (SSE/STDIO)
       │
┌──────v──────┐    ┌───────────────────┐
│  MCP Server │    │   Human UI        │
│  (AI 入口)   │    │  React + shadcn  │
└──────┬──────┘    └────────┬──────────┘
       │                    │ GraphQL + WS
       └────────┬───────────┘
                ▼
      ┌──────────────────┐
      │   Service Layer  │
      │  (Go + GORM)     │
      └──────────────────┘
```

## 接口

| 入口 | 地址 | 用途 |
|------|------|------|
| Web UI | `http://localhost:8080` | 人类操作界面（浏览器） |
| GraphQL API | `POST /graphql` | 全量 CRUD + Subscription |
| 健康检查 | `GET /health` | 运维探测 |

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| CHICK_DB_DRIVER | sqlite3 | sqlite3 / postgres |
| CHICK_DB_DSN | file:dev.db | 连接串 |
| CHICK_PORT | 8080 | HTTP 端口 |
| CHICK_BOOTSTRAP_TOKEN | (自动生成) | Agent 首次注册令牌 |

## MCP Tools

| Tool | 说明 |
|------|------|
| create_project | 创建项目 |
| register_agent | 注册 Agent（AI/Human） |
| login_agent | 登录获取凭证 |
| create_issue | 创建 Issue（自动编号） |
| add_comment | 添加评论 |
| assign_issue | 指派 Agent |
| transition_issue | 状态流转 |
| search_issues | 搜索 Issue |
| list_agents | 列出 Agent |
| agent_heartbeat | 心跳保活 |
| check_notifications | 检查通知 |
| submit_feedback | 提交反馈 |
| list_feedback | 查询反馈 |
| list_skills | 列出技能 |
| run_skill | 执行技能 |

## 运行测试

```bash
# 后端测试
go test ./... -count=1

# 前端测试
cd ui && npm test
```

## 了解更多

- [design/](design/) — 系统架构、API 设计、数据模型、路线图
- [ui/DESIGN.md](ui/DESIGN.md) — 前端设计风格：色彩、字体、组件规范、手机优先策略
- [AGENTS.md](AGENTS.md) — 贡献规范
