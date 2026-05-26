---
title: Workflows
description: Constrain LLM behavior with structured, step-by-step workflows
slug: workflows
weight: 17
---

Dolphin's **Workflow** subsystem lets you define structured, step-by-step procedures that the LLM must follow exactly. Workflows are stored as markdown files with YAML frontmatter and are loaded automatically.

## Overview

A workflow is a hard constraint, not a suggestion. When a task matches a workflow, the LLM must use `run_workflow` and follow every step — it cannot improvise or skip steps. This is useful for:

- **Deployment checks**: Standardized health checks before/after deployments
- **Code reviews**: Consistent review checklists across the team
- **Incident response**: Reliable, repeatable runbooks
- **Compliance**: Auditable, mandatory procedures

## File Structure

Workflows are stored in `.dolphin/workflows/`, one directory per workflow:

```
.dolphin/workflows/
  deploy-check/
    WORKFLOW.md
  code-review/
    WORKFLOW.md
```

Each `WORKFLOW.md` file uses YAML frontmatter:

```markdown
---
name: deploy-check
description: Check deployment health
---

When I ask you to run the deployment check, follow these steps:
1. Run `kubectl get pods --all-namespaces`
2. Run `kubectl get nodes`
3. Summarize findings
```

The directory name serves as the workflow name; the `name` field in frontmatter overrides it if present.

## Tools

The LLM has 8 MCP tools for workflow management, split into two tiers:

### Always Available

| Tool | Description |
|------|-------------|
| `list_workflows` | List all available workflows with descriptions |
| `load_workflow` | Load a workflow's full content including all steps |
| `run_workflow` | Execute a workflow by name — the LLM MUST follow every step |

### Self-Evolution Only

These require `flags.self_evolution: true`:

| Tool | Description |
|------|-------------|
| `create_workflow` | Create a new workflow |
| `update_workflow` | Update an existing workflow |
| `delete_workflow` | Permanently delete a workflow |
| `enable_workflow` | Re-enable a disabled workflow |
| `disable_workflow` | Disable a workflow (preserves files) |

## Agent Visibility

Workflows can be scoped to specific agents via the `workflows` field in agent definitions. If the allowlist is empty, all workflows are visible.

## Configuration

```yaml
workflows:
  dir: .dolphin/workflows    # workflow directory (default)
```

Multiple directories are supported — the first is writable, additional ones are read-only (e.g., project-level + user-level `~/.dolphin/workflows`).

## CLI Commands

| Command | Description |
|---------|-------------|
| `dolphin workflow list` | List all workflows |
| `dolphin workflow show <name>` | Show a specific workflow |
| `dolphin workflow new <name>` | Create a new workflow from template |
| `dolphin workflow delete <name>` | Delete a workflow |
| `dolphin workflow disable <name>` | Disable a workflow |
| `dolphin workflow enable <name>` | Re-enable a workflow |

## In-Session Commands

Use `/workflow` in a Dolphin session to list, create, delete, or show workflows. Subcommands: `new`, `delete`, `show`.

> Last modified: 2026-05-26
