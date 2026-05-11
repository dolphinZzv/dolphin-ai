# 工作流与协作模式

## 1. Issue 生命周期

```
OPEN ──► IN_PROGRESS ──► REVIEW ──► CLOSED_COMPLETED
 │                            │
 │                            ▼
 │                      CLOSED_NOT_PLANNED
 ▼
BLOCKED ──► IN_PROGRESS
```

## 2. Agent 能力匹配

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
              │ 1. 入等待队列  │
              │ 2. 通知 Project Owner│
              │ 3. T+30min 无人接手 │
              │    则标记 blocked    │
              └──────────────┘
```

### 兜底策略

| 触发条件 | 动作 | 说明 |
|----------|------|------|
| 匹配结果为 0 | 入等待队列 | 定时器每 5 分钟重试 |
| 等待 > 15 分钟 | 通知 Project Owner | @Owner："Issue #N 无人认领" |
| 等待 > 30 分钟 | 标记 BLOCKED | Issue.state = blocked，通知所有成员 |
| Owner 手动指派 | 跳过匹配直接 assign | addAssignee 绕过匹配引擎 |

## 3. 协作模式

### 直接对话 (Issue as Channel)

Agent 之间在 Issue 内通过 Comment 对话，围绕工作单元展开上下文讨论：

```
Issue: #42 — 重构 parser 模块

┌──── Agent A ────┐
│ 我建议把 parser 拆成两个文件 │
└──────┬──────────┘
       │
┌──── Agent B (Human) ────┐
│ 同意。lexer 用什么接口？  │
└──────┬──────────────────┘
       │
┌──── Agent A ────────────────────┐
│ 用 TokenStream 接口             │
│ (tool_call: edit_file lexer.go) │
└──────┬─────────────────────────┘
       │
┌──── Agent B ────────────────┐
│ approved → transitionIssue  │
└─────────────────────────────┘
```

### 子任务分解 (Issue Tree)

```
Issue #40: 实现用户登录模块
  ├── Issue #41: 设计数据库表
  ├── Issue #42: 实现 API 端点
  │   ├── Issue #43: POST /login
  │   └── Issue #44: POST /register
  ├── Issue #45: 写单元测试
  └── Issue #46: 安全审计
```

### Agent 协商

Agent 可 reassign Issue 或协商 deadline，通过 Comment 的结构化数据完成：

```
Agent-Coder:
  addComment(issue: #42, body: "需要 DB 知识，我不擅长",
             structuredData: { suggestedAssignee: "Agent-DB" })
```

## 4. 事件驱动架构

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  GraphQL    │───►│  Event Bus       │───►│  Handlers       │
│  Resolver   │    │  (in-memory)     │    │  (匹配/通知/...) │
│  MCP Tool   │    │  channel + fanout│    │                 │
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
                          ▼                         ▼
                   ┌──────────────────────────────────────┐
                   │  GraphQL Subscription Pub/Sub        │
                   │  (Human UI 专用, AI Agent 不走这个)   │
                   └──────────────────────────────────────┘
```

### 事件类型

| 事件 | 触发 | 消费者 |
|---|---|---|
| Issue.Created | createIssue | 能力匹配引擎 → 通知 Agent |
| Issue.StateChanged | transitionIssue | Notification → 相关方 |
| Comment.Added | addComment | Notification → 轮询 Agent |
| Agent.StatusChanged | updateAgentStatus | 能力匹配引擎重新计算 |
| Issue.AssigneeChanged | add/removeAssignee | Notification |

## 5. Human-in-the-loop 审批

```
Agent A (coding agent):
  1. transitionIssue(id, REVIEW)
  2. addComment(body: "PR ready, please review")

Human / Review Agent:
  ├── APPROVAL → transitionIssue(id, CLOSED_COMPLETED)
  └── REJECTION → transitionIssue(id, IN_PROGRESS) + comment
```
