# 数据模型

## 1. 核心模型关系

```
Project
  ├── Members (ProjectMember → Agent)
  ├── Issues (→ Creator, Assignees, Comments, Timeline)
  ├── Labels
  ├── Milestones
  └── Skills

Agent
  ├── CreatedIssues
  ├── AssignedIssues (via IssueAssignee)
  └── Comments

Issue
  ├── Creator (Agent)
  ├── Assignees (IssueAssignee → Agent)
  ├── Labels
  ├── Comments
  ├── TimelineEvents
  ├── Parent/Children (树状分解)
  └── Milestone
```

## 2. 模型概览

完整 GORM 模型定义见 `internal/models/`，以下是关键字段摘要：

### Project
`id, name, description` → HasMany: Members, Issues, Labels, Milestones, Skills

### Agent
`id, name, kind(ai/human/hybrid), status, external_id, capabilities(jsonb), metadata(jsonb), last_seen_at`

### Issue
`id, number(project内自增), project_id, title, description, state, priority, creator_id, parent_id, milestone_id, due_date, structured_output(jsonb)`

### Comment
`id, issue_id, author_id, parent_id, body, content_type(markdown/tool_call/tool_result/...), tool_call_data(jsonb), structured_data(jsonb)`

### IssueAssignee
`issue_id, agent_id, state(pending/in_progress/completed/blocked)` — UNIQUE(issue_id, agent_id)

### Label
`id, project_id, name, color, capability, group` — 支持能力匹配映射

### Milestone
`id, project_id, title, description, state(open/closed), due_date`

### Skill
`id, project_id, name, description, definition(YAML)` — MCP Tool 编排定义

### TimelineEvent
`id, issue_id, actor_id, event_type, payload(jsonb)` — 事件溯源

### Feedback
`id, target_type(issue/comment/agent/assignment), target_id, author_id, rating(1-5), body`

## 3. 数据库驱动策略

| 环境 | 驱动 | DSN 示例 |
|------|------|---------|
| 开发 | sqlite3 | `file:dev.db` |
| 生产 | postgres | `postgres://user:pass@host:5432/db` |

**切换方式：** 环境变量 `DB_DRIVER` + `DB_DSN`，Service/Repository 层零改动。

| 特性 | SQLite | PostgreSQL |
|------|--------|------------|
| 自增 ID | INTEGER PK | SERIAL |
| JSON | TEXT(序列化) | jsonb |
| 并发 | 写锁 | MVCC |
| 迁移 | AutoMigrate | goose |

## 4. 测试数据策略

| 场景 | DB | 方式 |
|------|-----|------|
| 单元测试 | SQLite :memory: | 快速运行无依赖 |
| 集成测试 | PostgreSQL | testcontainers-go |
| E2E 测试 | SQLite :memory: | 独立实例 |
