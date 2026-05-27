# Proposal: Unified Command Registry

> **Status**: Draft  
> **Last modified**: 2026-05-27

## Summary

当前命令/工具定义散落在 6 个位置。相同逻辑的命令在 Cobra CLI、Console REPL、LLM Tool 三处独立定义。目标是中央 registry + Cobra 统一，每个命令写一次。

## Architecture

```
用户输入
   ├── CLI:  dolphin workflow list  ────┐
   └── REPL: /workflow list       ──────┤
                                         v
                                 ┌───────────────┐
                                 │ cobra.Command  │  ← 唯一 handler
                                 │ RunE+OutOrStd │
                                 └───────┬───────┘
                                         │
              ┌──────────────────────────┼──────────────────────────┐
              v                          v                          v
        CLI 直接执行              REPL 重定向输出           LLM Tool 包装
        os.Stdout                → transport.UserIO      → *mcp.ToolResult
```

## Key Design

### `internal/registry/`

```
internal/registry/
    spec.go      # CommandSpec + Category + ConsoleSignal
    registry.go  # Registry 容器
    console.go   # Console 适配器: UserIO → cobra 执行
    tool.go      # LLM Tool 适配器: cobra → ToolDef
```

### CommandSpec

```go
type CommandSpec struct {
    Cobra         *cobra.Command   // 命令的唯一定义
    Category      Category         // 分组
    ToolSchema    map[string]any   // LLM Schema（nil 时自动推断）
    ToolName      string           // Tool 名称（默认用 cobra.Use）
    SelfEvolution bool             // 仅在 SelfEvolution 启用时注册 tool
    Signal        ConsoleSignal    // exit/new/reload 等流程控制信号
}
```

### Console 适配

REPL 输入 `/workflow show x`:
1. 解析为 `["workflow", "show", "x"]`
2. Registry 找到对应的 cobra command
3. `SetOut(consoleWriter{io})` + `SetErr(consoleWriter{io})`
4. 位置参数 → flag 映射（handler 用位置参数，CLI 可额外加 flag）
5. 执行 `cmd.RunE(cmd, args)`
6. 检查 `spec.Signal`

### Tool 适配

从 cobra command 的 flags 自动推断 JSON Schema，包装 `RunE` 将输出捕获为 `*mcp.ToolResult`。

## Non-Cobra Parts

- **exit/new/reload** 的流程控制保留 `ConsoleSignal` 机制
- **REPL-only 命令**（`/cancel`, `/forget`, `/feedback`, `/context`, `/transport`）注册为 cobra 但 `Hidden: true`，仅 REPL 可调用
- **CLI-only 命令**（`version`, `update`, `install`）不注册到 registry
- **User-defined /commands** 文件驱动，不走 cobra

## Migration Phases

| Phase | Scope | Key Files |
|---|---|---|
| P0 | Bootstrap registry | `internal/registry/*` |
| P1 | Pilot: workflow | workflow.go, cmd/workflow.go, coordinator_console.go |
| P2 | config + sessions | cmd/config.go, cmd/sessions.go |
| P3 | skills + commands | cmd/skills.go, coordinator_tools.go |
| P4 | agent + mcp + cron | 剩余命令 |
| P5 | 接口收紧 | 删除死代码, 更新注释, 清理已迁移 handler |

<!-- last-modified: 2026-05-28 -->
