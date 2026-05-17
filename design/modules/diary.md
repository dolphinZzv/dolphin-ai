# Session Diary (`internal/diary/` — v0.3)

将会话 Summary 汇总为时间层级:

- **Day** → **Week** → **Month** → **Year**
- 每日 20:00 Actor 同步
- 存储管理: 各层级保留数限制 + 总大小限制
- `prune.go` — 超过阈值时自动裁剪旧条目

<!-- last-modified: 2026-05-13 -->
