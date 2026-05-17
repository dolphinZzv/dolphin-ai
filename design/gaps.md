# Design Gaps

## Resolved (previously identified, now implemented)

- LLM Provider 抽象 — ✅ Provider 接口 + OpenAI + Anthropic
- Tool Registry + MCP Server — ✅ 4 内置工具 + 外部 MCP 服务器
- 安全与沙箱 — ✅ Shell 白名单/超时/路径检查 + SSH 密码自动生成
- 会话并发管理 — ✅ Manager + Reaper
- 结构化日志 — ✅ zap + lumberjack
- 信号处理 — ✅ oklog/run 优雅关闭

## Remaining

| Gap | Impact | Suggestion |
|-----|--------|------------|
| Agent 包耦合过重 | `internal/agent` 依赖 8+ 包；`context.go` 是为避免循环引用的 wrapper | 将 metrics/hook/event 的 agent 级逻辑抽离；消除 context wrapper |
| Config 结构体膨胀 | 15+ 子配置在同一结构体 | 按域拆分为接口段 |
| 无速率限制 | `golang.org/x/time` 在 go.mod 但未使用 | LLM 调用和 MCP 工具执行加上限流 |
| Config Handler 无权限 | 运行时配置修改无审计 | 增加操作审计 + 权限校验 |
| 无分布式支持 | Session/Metrics/Event 均为单进程内存模型 | 无法水平扩展 |
| 无持久化队列 | EventBus channel 满时直接 drop | 需要背压或持久化机制 |

<!-- last-modified: 2026-05-13 -->
