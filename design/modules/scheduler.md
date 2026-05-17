# Scheduled Tasks (`internal/scheduler/` — v0.3)

## CRONTAB.md

YAML frontmatter + Markdown body, 与 Skill 文件格式一致：

```markdown
---
name: auto-commit
schedule: "0 18 * * 1-5"
description: 每天下午6点自动提交
enabled: true
---

Run git add -A, git commit -m "auto commit", and git push.
```

## Mechanism

- `robfig/cron/v3` 解析标准 cron 表达式
- 每 30s tick 检查到期任务
- `dueCh (chan CronTask, buffer 100)` → Coordinator 后台 goroutine 异步执行
- 每个任务独立 session 执行
- 结果存入 `cronMgr.AddResult()`

## Fault Tolerance

- 文件不存在 → 创建空文件 (带说明 header)
- 解析失败 → 跳过该条目 (warn)
- 整体损坏 → 备份为 .bak, 创建新文件
- 不阻塞启动

## MCP Tools

`add_cron_task` / `remove_cron_task` / `list_cron_tasks` / `toggle_cron_task`

## CLI

`/crontab` — 查看任务状态

<!-- last-modified: 2026-05-13 -->
