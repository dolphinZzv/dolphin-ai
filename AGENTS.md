# Agent Collaboration Platform — AGENTS.md

本文件供 AI Agent 和人类开发者遵守，确保协作的一致性。

## 项目概览

Go + GraphQL 多 Agent 协作系统。Issue 模型作为核心工作单元，Agent（含 Human）通过 Comment 交流，通过 Label→Capability 匹配。

## 目录结构

```
cmd/server/          # 入口
internal/
  graph/resolver/    # GraphQL handler（薄层）
  mcp/tools/         # MCP tool handler（薄层）
  service/           # 业务逻辑
  repository/        # GORM 数据访问
  models/            # 纯数据结构
  matching/          # 能力匹配引擎
  notifications/     # 通知服务
  events/            # 事件总线
  server/            # 服务初始化
```

## 分层依赖规则

```
Handler → Service → Repository → DB
```

- Handler（graph/ + mcp/）只做参数解析 → 调 Service → 格式化返回。不写业务逻辑。
- Service 面向 Repository 接口编程，依赖通过构造函数注入。
- Repository 只做 GORM CRUD，不包含业务逻辑。
- 禁止反向依赖和越层调用（resolver → repository 等）。

## 代码规范

| 项 | 规则 |
|---|---|
| 包命名 | 小写单数 (service, matching) |
| 文件命名 | snake_case (issue.go, project_member.go) |
| 构造器 | New 前缀 (NewIssueService) |
| Context | 作为第一个参数 |
| 错误 | 用 fmt.Errorf wrap 上下文 |
| 日志 | slog，不用 fmt.Println |
| 枚举 | 常量 + iota |
| JSON 字段 | snake_case |
| GraphQL | PascalCase 类型, camelCase 字段 |

## 验证原则

所有验证必须代码化（禁止人工检查），验证代码统一放在 `verif/` 目录：

```
verif/
  constraints_test.go   # DB 约束验证（唯一索引、外键、非空）
  preloads_test.go      # GORM 预加载验证
  verif_test.go         # 门禁验证：编译、测试、代码规范
```

- 每次 schema 变更后运行 `go test ./verif/ -v`
- CI 门禁必须包含 `go test ./verif/`
- 验证失败代表设计或实现有误，不允许合并

## 修改流程

1. 先读 design/ 理解设计意图
2. 读 AGENTS.md 了解规范
3. 检查 models/ 中对应数据结构是否存在
4. 按依赖方向修改：models → repository → service → handler
5. 如果改 Service 签名，同步更新所有调用方（server.go + 测试）
6. 测试：go test ./internal/... -count=1
7. 编译：go build ./...

## EventBus 使用

- Service 通过 `eventBus.Publish()` 发布领域事件
- matching/ 和 notifications/ 通过 `Subscribe()` 消费事件
- 测试中用 `PublishSync()` 替代 `Publish()` 确保同步执行

## DB 双驱动

- SQLite（开发）：`CHICK_DB_DRIVER=sqlite3 CHICK_DB_DSN="file:dev.db"`
- PostgreSQL（生产）：`CHICK_DB_DRIVER=postgres CHICK_DB_DSN="postgres://..."`
- @> JSONB 操作符仅在 PostgreSQL 中生效
- ILIKE 仅在 PostgreSQL 中生效，SQLite 用 LIKE
