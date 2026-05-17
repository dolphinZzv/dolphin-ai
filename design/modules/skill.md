# Skills System (`internal/skill/` — v0.3)

## Format

Markdown 文件 (YAML frontmatter + 指令内容)，存储在 `.dolphin/skills/`:

```markdown
---
name: code-review
description: Review Go code for bugs, style, and performance
call_count: 0
---

Focus on: logic errors, race conditions, deadlocks, API misuse...
```

## Discovery

- 多目录加载: `~/.dolphin/skills/` + `.dolphin/skills/`
- 渐进披露: Top 10 (按 call_count) + `search_skills` / `load_skill`
- 后台 30s 热重载 watcher (ticker polling)

## Stats

自动跟踪 `call_count`, `last_called_at`；`TopSkills(n)` 排序返回。

<!-- last-modified: 2026-05-13 -->
