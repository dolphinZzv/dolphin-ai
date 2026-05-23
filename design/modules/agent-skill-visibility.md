# Agent-Specific Skill & Workflow Visibility (v0.3)

## Problem

Current skills and workflows are **globally visible** to all agents. There is no mechanism to restrict which skills or workflows a particular sub-agent can see or use. This makes it impossible to:

- Expose different skills to different agents (e.g., `code-review` skill only for `reviewer` agent)
- Prevent an agent from loading a skill not relevant to its role
- Scope workflow access per agent

## Design

### AgentDef扩展

在 `agent.yaml` 中新增两个可选字段：

```yaml
name: reviewer
tools:
  - shell
skills:              # 可选：此 agent 可见的 skill 列表，空 = 全部可见
  - code-review
  - security-scan
workflows:           # 可选：此 agent 可见的 workflow 列表，空 = 全部可见
  - review-flow
```

### Manager层过滤

Skill 和 Workflow Manager 各新增两个方法：

```go
// ListForAgent 返回 agent 可见的条目列表。
// allowed 为空/ nil = 返回全部（向后兼容）。
func (m *Manager) ListForAgent(allowed []string) []*Skill
func (m *Manager) GetForAgent(name string, allowed []string) (*Skill, bool)
```

Workflow Manager 同理。

### Tool Handler 包装

在 `AgentPool.Add()` 中，创建 sub-agent 的工具注册表时，根据 `AgentDef.Skills` / `AgentDef.Workflows` 字段包装对应的 tool handler：

- **`search_skills`/`load_skill`**：当 `AgentDef.Skills` 非空时，包装 handler，只返回/允许列表内的 skill
- **`list_workflows`/`load_workflow`/`run_workflow`**：当 `AgentDef.Workflows` 非空时，同理

包装通过创建 closure handler 实现，在过滤后的工具注册表副本上替换原 handler，不影响 Coordinator 自身的工具。

### 向后兼容

- `AgentDef.Skills` / `AgentDef.Workflows` 为空时，行为不变（全部可见）
- Coordinator（主 agent）不受影响，始终可见全部
- 已有 agent.yaml 无需修改

## 并发安全

Skill/Workflow Manager 新增方法使用 `RLock`（读取操作），与现有 `Get`/`List` 一致。

## 文件变更

| 文件 | 变更 |
|------|------|
| `internal/agent/agent_types.go` | `AgentDef` 加 `Skills` / `Workflows` 字段 |
| `internal/skill/skill.go` | 加 `ListForAgent` / `GetForAgent` |
| `internal/subsystem/workflow/workflow.go` | 加 `ListForAgent` / `GetForAgent` |
| `internal/agent/agent_pool.go` | `Add` 方法加 skill/workflow 包装逻辑 |
| `internal/agent/coordinator_tools.go` | `pool.Add` 调用处传新参数 |
| `cmd/root.go` | `pool.Add` 调用处传 skill/workflow manager |
| 测试文件 | 集成测试覆盖 |

## 集成测试方案

### Skill 过滤测试
1. 创建 Skill Manager，注册 3 个 skill：`a`、`b`、`c`
2. `ListForAgent(nil)` → 返回 3 个
3. `ListForAgent(["a", "c"])` → 返回 2 个
4. `GetForAgent("a", ["a", "c"])` → 找到
5. `GetForAgent("b", ["a", "c"])` → 没找到

### Workflow 过滤测试（同上）

### Agent pool 集成测试
1. 创建带 skill/workflow 限制的 AgentDef
2. 调用 pool.Add，验证 sub-agent 的 tool registry 中 handler 是否正确过滤
3. 创建不带限制的 AgentDef，验证全部可见（向后兼容）

### Coordinator 工具过滤测试
1. 构建一个 Coordinator 场景，注册 agent 带 skill 限制
2. 模拟调用 `load_skill`，验证受限的 skill 被拒绝
3. 模拟调用 `search_skills`，验证只返回允许的 skill

> Last modified: 2026-05-23
