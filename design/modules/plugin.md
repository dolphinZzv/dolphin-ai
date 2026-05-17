# Plugin System (`internal/plugin/` — v0.3)

## Interface

```go
type Plugin interface {
    Name() string
    Register(reg *Registry)  // reg.AddHook() / reg.AddEvent()
}
```

## Manager

- `NewManager(hooks, eventBus)` → 创建
- `LoadScripts(dir)` → 从文件系统加载脚本插件
- `Activate()` → 调用所有 Plugin.Register()，通过 `ApplyTo()` 注入 hook + event 系统

## Script Loading

`scripts.go` — 从插件目录加载 `.so` 或脚本文件 (可扩展)。

<!-- last-modified: 2026-05-13 -->
