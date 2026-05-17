# Context Building (`internal/context/`)

## Builder

`Builder.Build()` / `Builder.BuildForAgent(agentName)` 按顺序拼接系统提示：

```mermaid
flowchart LR
    subgraph System Prompt
        P1["PREFACE.md<br/>(//go:embed)"]
        P2["BUILTIN_SKILLS.md<br/>(//go:embed)"]
        P3["AGENTS.md<br/>(agent → project → user → system)"]
        P4["RULES.md"]
        P5["USER.md"]
        P6["SYSTEM.md"]
    end

    P1 --> P2 --> P3 --> P4 --> P5 --> P6 --> Out["System Prompt"]
```

## BuildForAgent

```mermaid
flowchart TB
    Start["BuildForAgent(name)"] --> Check{".dolphin/agents/<name>/<br/>AGENTS.md exists?"}
    Check -->|Yes| AgentDir["使用 agent 专属上下文"]
    Check -->|No| Fallback["fallback 项目/用户/系统目录"]
    AgentDir --> Out["系统提示 (含 AGENTS.md)"]
    Fallback --> Out

    style Start fill:#e1f5fe
    style Out fill:#c8e6c9
```

## Caching

文件内容按 mtime 缓存，仅在文件变更时重新读取。

## Agent-Specific Context

`BuildForAgent("reviewer")` 会在 `.dolphin/agents/reviewer/` 下优先查找 AGENTS.md/RULES.md/USER.md，fallback 到项目/用户/系统目录。

<!-- last-modified: 2026-05-17 -->
