# Issue 处理流程

**集成 Chick 任务管理系统，与 change-flow 联动。**

## 状态机

```
open → in_progress → review → closed_completed ←─┐
                   → blocked ──→ open (解除阻塞) │
                   → closed_not_planned           │
                                         closed_completed → open (复发)
```

## 流程图

```text
┌────────────────────────────────────────────────────┐
│ 1. 发现 Bug / 需求 / 文档问题                       │
│    ├─ Agent 自行发现 → 直接创建 issue              │
│    ├─ 用户提出 → 创建 issue 并关联原始对话          │
│    ↓                                               │
│ 2. 分类与定级                                       │
│    ├─ 类型：bug / feature / docs / chore            │
│    ├─ 优先级：critical / high / medium / low        │
│    │  ├─ critical: 核心功能不可用、数据安全         │
│    │  ├─ high: 主要功能受损、严重 UX 问题           │
│    │  ├─ medium: 次要功能问题、文档偏差             │
│    │  └─ low: 拼写错误、微小改进建议               │
│    ↓                                               │
│ 3. 分配（Chick_assign_issue）                       │
│    ├─ 先用 Chick_list_agents 获取 agent 列表和 ID  │
│    ├─ 明确责任人（agentId）                         │
│    ├─ 无责任人 → 留 unassigned，等待认领            │
│    ↓                                               │
│ 4. 确认可复现 / 需求清晰                          │
│    ├─ 不可复现 → 添加评论说明 → closed_not_planned │
│    ├─ 需澄清 → 评论追问 → 等待回复（存 blocked）   │
│    ↓                                               │
│ 5. 进入开发流程（change-flow）                      │
│    ├─ 将 issue 编号写入 todo/ 或 feature/           │
│    ├─ 严格按 change-flow.md 9 步执行               │
│    ├─ commit message 引用 issue: "fix(#168): ..."  │
│    └─ 设计文档中注明 "关联 issue #xxx"              │
│    ↓                                               │
│ 6. 状态流转（Chick_transition_issue）               │
│    ├─ 开始修复 → in_progress                        │
│    ├─ 等待用户反馈 / 外部依赖 → blocked             │
│    ├─ 代码审查通过 → review                         │
│    ↓                                               │
│ 7. 验收与关闭                                       │
│    ├─ 用户确认修复 → closed_completed               │
│    ├─ 非问题 / 不采纳 → closed_not_planned          │
│    └─ 关闭时添加评论：关闭原因 + 关键 commit        │
└────────────────────────────────────────────────────┘
```

## 关键规则

| # | 规则 |
|---|------|
| 1 | 所有 issue 必须设置 **类型** 和 **优先级** |
| 2 | 状态流转必须使用 `Chick_transition_issue`，不允许直接 close |
| 3 | 代码变更必须关联 issue：commit message 格式 `fix(#168): msg` / `feat(#167): msg` |
| 4 | 不可复现的 bug → 评论说明原因后再 `closed_not_planned` |
| 5 | Blocked 状态必须附带阻塞原因和期望的解除条件 |
| 6 | 同一次对话中批量发现的关联问题，应在各 issue 评论中互相引用 |
| 7 | `closed_completed` 的问题复发 → 重新 `Chick_transition_issue` 为 `open`，不能开新 issue |
| 8 | Issue 标题和描述使用**中文**，commit message 使用**英文**。涉及文档的 issue 必须标注 `[zh]`/`[en]`/`[zh/en]` |

## 状态定义

| 状态 | 含义 | 操作者 |
|------|------|--------|
| `open` | 已创建，待处理 | Agent / 用户 |
| `in_progress` | 正在修复中 | Agent |
| `blocked` | 等待外部输入 | Agent |
| `review` | 代码审查中 | Agent（自审） |
| `closed_completed` | 已修复关闭。复发时重新 `→ open` | Agent / 用户 |
| `closed_not_planned` | 不修复 | Agent / 用户 |

## 评论规范

添加评论时（`Chick_add_comment`）应包含：

- **Bug**：复现步骤 + 环境信息 + 脱敏的相关配置/日志
- **Feature**：使用场景 + 期望行为 + 参考设计（如有）
- **Docs**：文档位置 + 当前内容 + 期望内容
- **关闭评论**：关闭原因 + 关键 commit hash + 影响范围

## issue → change-flow 联动

```
                                 change-flow
issue (open) ──────────────────→ 1. todo/feature 归档（引用 issue #）
↓                                 2. Agent 自审需求
│  Chick_transition_issue         3. 输出设计文档
│  → in_progress                  4. Agent 自审设计
│                                 5. 创建分支写代码
│                                 6. 单元测试
│                                 7. Agent 自审代码
│  提交 commit: "fix(#168): ..."  8. 提交代码
│  Chick_transition_issue
│  → review                      
│  代码审查通过                   
↓                                 9. 用户确认合并
issue → closed_completed ←──────  (merge → close)
```

对应关系：

| issue 状态 | change-flow 步骤 | 触发条件 |
|-----------|-----------------|---------|
| `open` | 1-4（归档 → 设计通过） | issue 创建 |
| `in_progress` | 5-8（编码 → 提交） | 开始写代码 |
| `review` | 7-8（自审 → 提交） | 代码自审通过，准备提交 |
| `blocked` | 任意步骤 | 等待外部输入 |
| `closed_completed` | 9（合并） | 用户确认合并 |
| `closed_not_planned` | — | 确认不修复 |

## 多语言规范

项目文档支持**中文**和**英文**。Issue 及相关评论应遵循以下规则：

| 场景 | 语言 | 说明 |
|------|------|------|
| Issue 标题 | **中文** | 优先用中文描述问题，除非该 issue 仅涉及英文文档 |
| Issue 描述 | **中文** | 用中文详细描述，便于团队快速理解 |
| 评论 | **与 issue 标题一致** | 中文 issue 全程用中文，英文 issue 全程用英文 |
| 涉及文档的 issue | **标注语言** | 在标题或描述开头标注 `[zh]` / `[en]` / `[zh/en]` |
| commit message | **英文** | 遵循 git 惯例，使用英文：`fix(#168): msg` |
| 设计文档 | **中文** | 技术设计文档用中文书写 |

示例：

```
# 标题（仅涉及英文文档）
[en] Quickstart: Dolphin > prompt mismatch with actual Coordinator mode

# 标题（涉及中英文双语文档）
[zh/en] Quickstart 文档与实际行为不一致：Dolphin > vs 协调器模式
```

## 通知处理

使用 `Chick_check_notifications` 定时检查通知：

- 新 issue 通知 → 评估分类和优先级
- 状态变更通知 → 了解关联 issue 进展
- 评论通知 → 及时回复
