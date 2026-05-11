# 核心概念与系统架构

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

**核心洞察：** 每个 Issue = 一个工作单元。Agent 之间通过 Comment 交流，通过 Assignment 流转任务，通过 Label 匹配能力。

**人与 AI 统一模型：** 人和 AI Agent 在系统中统一为 Agent 类型，只是 `kind` 不同。

**Project = 协作边界：** 所有 Issue、Label、Milestone、Skill 都属于一个 Project。Agent 通过 Project Membership 获得权限。

## 2. 系统架构图

```
                         AI Assistant
                      (Claude, GPT, etc.)
                              |
                         MCP Protocol
                              |
                     MCP Client (Host)
                              │
       Platform               │
          │                   ▼
          │     Human UI (React)
          │  shadcn + urql + Vite
          │
          │      Entry Points (peer, not chain)
          │  +--------------------------+  +--------------------------+
          │  |     GraphQL API          |  |     MCP Server           |
          │  |  (gqlgen + WS)           |  |  (SSE / STDIO)           |
          │  |  [Human UI / SDK入口]     |  |  [AI Assistant 入口]     |
          │  +------------+-------------+  +------------+-------------+
          │               |                             |
          │               |   +---------------------+   |
          │               |   | MCP Tool Handler    |   |
          │               |   | (thin adapter)       |   |
          │               |   +----------+----------+   |
          │               |              |              |
          │  +------------+--------------+--------------+-------------+
          │  |                 Service Layer (共享)                  |
          │  |  Project / Issue / Agent / Skill / Workflow          |
          │  |  Matching Engine / Event Bus / Notification           |
          │  +---------------------------+-------------------------+
          │                              |
          │  +---------------------------v-------------------------+
          │  |                     Data Layer                      |
          │  |  SQLite (dev) / PostgreSQL (prod)                   |
          │  +-----------------------------------------------------+
```

**MCP 和 GraphQL 调用同一份 Service：** 两者都是薄适配层，职责只做参数解析 → 调用 Service → 格式化返回。

## 3. 技术选型

| 层 | 技术 | 理由 |
|---|---|---|
| 语言 | Go 1.22+ | 强并发、编译快、部署简单 |
| API | GraphQL (gqlgen) | 强类型 Schema、Subscription、灵活查询 |
| 前端 | React 19 + TypeScript + Vite | 现代 SPA 框架，HMR 快速 |
| UI 组件 | shadcn/ui + Tailwind CSS | Radix 原语 + 无障碍 + 暗色模式 |
| ORM | GORM | SQLite + PostgreSQL 双驱动 |
| DB | SQLite (dev) / PostgreSQL (prod) | 开发零依赖，生产全 ACID |
| 消息总线 | in-memory EventBus | Phase1-3 单实例够用 |

## 4. 代码分层

```
                    Handler Layer
  ┌─────────────────────────┐  ┌──────────────────────────┐
  │ graph/resolver/         │  │ mcp/                     │
  │ GraphQL 适配             │  │ MCP Tool/Resource/Prompt │
  └──────────┬──────────────┘  └───────────┬──────────────┘
             职责: 协议适配、参数解析、调用 Service
             禁止: 写业务逻辑、直接操作 DB
              │                            │
              ▼                            ▼
                    Service Layer
  ┌──────────────────────────────────────────────────────┐
  │  Project / Issue / Agent / Workflow / Matching      │
  │  EventBus / Notification / Skill                    │
  │  职责: 业务编排、状态机、匹配算法                      │
  │  依赖: Repository 接口（不是具体实现）                  │
  └──────────────────────────────────────────────────────┘
                           │
                           ▼
                    Repository Layer
  ┌──────────────────────────────────────────────────────┐
  │  GORM CRUD、复杂查询、事务管理                         │
  │  依赖: *gorm.DB                                       │
  └──────────────────────────────────────────────────────┘

依赖方向（严格单向）: Handler → Service → Repository → DB
```

**Handler 层职责：** 协议适配、参数解析、调用 Service、错误格式化
**Service 层职责：** 业务编排、状态机、事件发布
**Repository 层职责：** GORM CRUD、复杂查询、事务管理
