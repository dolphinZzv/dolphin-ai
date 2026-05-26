---
title: 工作流
description: 通过结构化步骤约束 LLM 行为
slug: workflows
weight: 17
---

Dolphin 的 **工作流（Workflow）** 子系统提供了一套机制，将 LLM 行为约束到固定的、逐步的 Markdown 文件中。工作流是强制性约束，而非建议性指南——当任务匹配到对应工作流时，LLM 必须使用 `run_workflow` 工具严格遵循每个步骤，不可即兴发挥或跳过步骤。

## 适用场景

- **部署检查**：部署前后的标准化健康检查
- **代码审查**：团队统一的审查清单
- **事件响应**：可靠、可重复的运行手册
- **合规审计**：可追溯的强制性流程

## 文件结构

工作流存储在 `.dolphin/workflows/` 目录下，每个工作流一个子目录：

```
.dolphin/workflows/
  deploy-check/
    WORKFLOW.md
  code-review/
    WORKFLOW.md
```

每个 `WORKFLOW.md` 文件使用 YAML frontmatter：

```markdown
---
name: deploy-check
description: 检查部署健康状态
---

当我要你执行部署检查时，请按以下步骤操作：
1. 执行 `kubectl get pods --all-namespaces`
2. 执行 `kubectl get nodes`
3. 汇总结果
```

目录名即为工作流名称；frontmatter 中的 `name` 字段可覆盖目录名。

## LLM 工具

LLM 有 8 个 MCP 工具用于管理工作流，分为两个层级：

### 始终可用

| 工具 | 说明 |
|------|------|
| `list_workflows` | 列出所有可用工作流 |
| `load_workflow` | 加载指定工作流的完整内容 |
| `run_workflow` | 执行工作流——LLM 必须严格遵循每一步 |

### 仅限自我进化

以下工具需要 `flags.self_evolution: true`：

| 工具 | 说明 |
|------|------|
| `create_workflow` | 创建新工作流 |
| `update_workflow` | 更新已有工作流 |
| `delete_workflow` | 永久删除工作流 |
| `enable_workflow` | 重新启用已禁用的工作流 |
| `disable_workflow` | 禁用工作流（保留文件） |

## Agent 可见性

工作流可以通过 agent 定义中的 `workflows` 字段限定可见范围。若 allowlist 为空，则所有工作流均可见。

## 配置

```yaml
workflows:
  dir: .dolphin/workflows    # 工作流目录（默认值）
```

支持多目录——首个为可写目录，其余为只读（例如项目级 `.dolphin/workflows` + 用户级 `~/.dolphin/workflows`）。

## CLI 命令

| 命令 | 说明 |
|------|------|
| `dolphin workflow list` | 列出所有工作流 |
| `dolphin workflow show <name>` | 显示指定工作流 |
| `dolphin workflow new <name>` | 从模板创建新工作流 |
| `dolphin workflow delete <name>` | 删除工作流 |
| `dolphin workflow disable <name>` | 禁用工作流 |
| `dolphin workflow enable <name>` | 重新启用工作流 |

## 会话内命令

在 Dolphin 会话中使用 `/workflow` 命令进行管理。子命令：`new`、`delete`、`show`。

> 最后修改: 2026-05-26
