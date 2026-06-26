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

## 开发工作流

**重要：用户在任意步骤提出的变更意见，都必须同步更新到设计文档**，确保设计始终反映最新共识。

**交互方式：当前阶段直接向用户提问。Chick 系统可用后，所有追问、确认、评估、合并询问等用户交互均通过 Chick Issue/Comment 进行。**

### 适用范围

| 变更类型 | 适用流程 |
|---|---|
| 影响主流程（核心业务逻辑、API、DB schema、事件流） | 走完整 10 步流程 |
| 不影响主流程（辅助功能、工具、配置、文档、格式化） | 跳过步骤 3-4，直接编码 |

> 判断标准：变更是否会影响 Issue → Agent → Comment 核心协作链路。拿不准就走完整流程。

### 完整流程（功能变更）

```
┌─────────────────────────────────────────────────┐
│ 1. 用户提出需求、问题或 Bug                       │
│    ├─ Bug → 先写入 todo/ 编号归档               │
│    ├─ Feature → 先写入 feature/ 编号归档        │
│    ↓                                            │
│ 2. Agent 自审需求（第一轮）                      │
│    ├─ 不清晰 → 追问用户澄清                      │
│    ↓                                            │
│   Agent 自审需求（第二轮）                      │
│    ├─ 仍不清晰 → 继续追问                        │
│    ├─ 通过                                      │
│    ↓                                            │
│ 3. 设计 — 输出设计文档到 design/ 或写清楚方案    │
│    ↑←───────────────┐                          │
│    ↓                 │                          │
│ 4. Agent 自审设计（第一轮）                      │
│    ├─ 有问题 ────────┘ 修改设计                  │
│    ↓                                            │
│   Agent 自审设计（第二轮）                      │
│    ├─ 仍有问题 → 修改设计 → 回第一轮            │
│    ├─ 通过                                      │
│    ↓                                            │
│ 5. 严格按设计写代码                              │
│    ├─ 按依赖方向：models → repository → service → handler
│    ├─ 用户反馈 → 同步更新设计文档                │
│    ↓                                            │
│ 6. 单元测试 — 所有新代码必须有测试               │
│    ├─ go test ./internal/... -count=1 100% 通过  │
│    ├─ 失败 → 回到步骤 5 修代码                   │
│    ↓                                            │
│ ► 提交代码（git commit）                         │
│    ↓                                            │
│ 7. 验证 — 将端到端/集成验证代码写入 verif/ 目录  │
│    ├─ go test ./verif/ -v 必须通过               │
│    ├─ 失败 → 回到步骤 5 修代码                   │
│    ↓                                            │
│ 8. Agent 自审代码（第一轮）                      │
│    ├─ 检查边界情况、错误处理、并发安全            │
│    ├─ 发现问题 → 回到步骤 5 → 回本轮重审        │
│    ├─ 发现设计问题 → 回到步骤 3 改设计           │
│    ↓                                            │
│   Agent 自审代码（第二轮）                      │
│    ├─ 仍有问题 → 回步骤 5 修改 → 回第一轮       │
│    ├─ 通过                                      │
│    ↓                                            │
│ 9. Agent 自评变更 — 影响范围、回滚方案、兼容性   │
│    ↓                                            │
│ 10. 询问用户是否改进或合并                       │
│    ├─ 合并 → Agent 创建 PR，请求合并             │
│    ├─ 改进 → 回到步骤 1，重新走流程              │
└─────────────────────────────────────────────────┘
```

### 迭代说明

- **步骤 6 测试失败** → 回到步骤 5 修代码，不必重走设计
- **步骤 7 验证失败** → 回到步骤 5 修代码，测试需重新全部通过
- **自审发现实现问题** → 回到步骤 5 修代码，修完后回本轮自审重审
- **自审发现设计缺陷** → 回到步骤 3 修改设计 → 步骤 4 重新自审 → 步骤 5 重新实现
- **第二轮自审仍有问题** → 按问题性质回到对应步骤修改，再回第一轮重新审查
- **用户在任何步骤提出变更** → 同步更新设计文档，按变更性质决定回溯到哪一步
- **步骤 10 用户确认合并** → Agent 创建 PR，请求合并
- **步骤 10 用户要求改进** → 回到步骤 1，重新走完整流程

### 关键规则

- **没有设计不写代码**：任何功能变更前必须有设计文档或方案描述
- **Bug 归档 todo/**：Bug 修复必须先写入 todo/ 编号归档，修复后更新结论
- **Feature 归档 feature/**：新增功能必须先写入 feature/ 编号归档，完成后更新结论
- **设计必须自审**：Agent 自审两轮，发现问题必须修改后再实施
- **代码必须自审**：Agent 自审两轮，检查实现与设计一致性、边界条件、错误处理
- **用户反馈同步设计**：用户在任何步骤提出的变更意见都必须同步更新到设计文档，确保设计与最终实现一致
- **测试必须 100% 通过**：`go test ./internal/... -count=1` 不能有失败，通过即提交
- **验证代码归 verif/**：集成/端到端验证统一放在 verif/，随变更一起提交
- **审查必须做**：实现后审查代码与设计的一致性、边界条件、错误处理
- **每一步都可回溯**：根据问题性质回到对应步骤修正

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

## 实现阶段指引（步骤 5 详细说明）

以下步骤对应开发工作流中的第 5 步（严格按设计写代码）：

1. 先读 design/ 理解设计意图
2. 读 AGENTS.md 了解规范
3. 检查 models/ 中对应数据结构是否存在
4. 按依赖方向实现：models → repository → service → handler
5. 如果改 Service 签名，同步更新所有调用方（server.go + 测试）
6. 实现完成后进入步骤 6（单元测试）

## EventBus 使用

- Service 通过 `eventBus.Publish()` 发布领域事件
- matching/ 和 notifications/ 通过 `Subscribe()` 消费事件
- 测试中用 `PublishSync()` 替代 `Publish()` 确保同步执行

## DB 双驱动

- SQLite（开发）：`CHICK_DB_DRIVER=sqlite3 CHICK_DB_DSN="file:dev.db"`
- PostgreSQL（生产）：`CHICK_DB_DRIVER=postgres CHICK_DB_DSN="postgres://..."`
- @> JSONB 操作符仅在 PostgreSQL 中生效
- ILIKE 仅在 PostgreSQL 中生效，SQLite 用 LIKE
