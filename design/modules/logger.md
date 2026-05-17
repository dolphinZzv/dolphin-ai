# Logger (`internal/logger/`)

- `go.uber.org/zap` — 结构化日志
- `gopkg.in/natefinch/lumberjack.v2` — 文件轮转 (大小/时间)
- Console encoder, ISO8601 时间格式, CapitalLevelEncoder
- Config: Level, File, MaxSize, MaxAge, MaxBackup
- 空 File → stderr 输出

<!-- last-modified: 2026-05-13 -->
