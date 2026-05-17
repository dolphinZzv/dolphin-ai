# Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| SSH | `golang.org/x/crypto/ssh` | 标准库，零外部依赖 |
| MQTT | `eclipse/paho.mqtt.golang` v1.4 | Go 生态事实标准 |
| CDP | `chromedp/chromedp` | 纯 Go 实现 |
| LLM SDK | provider_openai + provider_anthropic | 覆盖主流模型，支持自定义端点 |
| Session format | JSONL 逐行追加 | O(1) 写入，tail/jq 实时查看，局部损坏不影响整体 |
| Concurrency | per-connection goroutine + 独立 LoopState | 无共享可变状态 |
| Actor model | `github.com/oklog/run` | 轻量，任一退出即关闭全部 |
| Logging | `go.uber.org/zap` + lumberjack | 高性能生产级轮转 |
| Line editing | `github.com/chzyer/readline` | 历史/补全/多行粘贴 |
| Config | `github.com/spf13/viper` | 多源合并，环境变量覆盖 |
| ID | `github.com/rs/xid` | 比 UUID 短，按时间排序 |
| Cron | `github.com/robfig/cron/v3` | Go 标准 cron 库 |
| [v0.2] Sub-agent IPC | async Channel (taskCh/resultCh) | Coordinator 永不阻塞 |
| [v0.2] Registry isolation | Clone() 复制 tools + stats map | 避免跨连接冲突 |
| [v0.2] Workspace isolation | 独立 workspace 目录 | Shell 在限定目录内执行 |
| [v0.3] Progressive disclosure | MostUsedTools(10) + search | 减少 LLM 上下文占用 |
| [v0.3] Skills | .dolphin/skills/ Markdown | 可扩展指令注入 |
| [v0.3] Cron tasks | CRONTAB.md YAML frontmatter | 与 Skills 相同文件模式 |
| [v0.3] Email transport | SMTP + IMAP v1 | 无需额外中间件 |
| [v0.3] Compressors | 5 strategies (drop/segment/tiered/incremental/topic) | 适配不同场景 |
| [v0.3] Hook | 优先级排序 + abortable | 插件可修改或阻止请求 |
| [v0.3] Event | channel 分发 + webhook 投递 | 非阻塞通知 |
| [v0.3] Plugin | Plugin.Register(reg) | 统一 hook + event 注册 |
| [v0.3] Metrics | 自定义 Counter/Gauge/Histogram | 轻量，无需 exporter |
| [v0.3] I18n | 键值映射 + LANG 检测 | 最小化依赖 |

<!-- last-modified: 2026-05-13 -->
