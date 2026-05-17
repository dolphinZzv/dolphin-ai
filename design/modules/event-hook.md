# Event Bus & Hook System

## Hook (Synchronous — `internal/hook/`)

```go
Points: session:start | session:end | user:input | llm:before | llm:after |
        tool:before | tool:after | response:before | error
```

- 按优先级排序执行
- `user:input`, `llm:before`, `tool:before` 可中止 (error → abort)
- Handler 可修改 UserInput, ToolArgs, Request

## Event (Asynchronous — `internal/event/`)

```go
Types: session:created | session:ended | user:message | llm:response |
       tool:called | tool:completed | compression | error | heartbeat |
       agent:dispatched | agent:completed | skill:loaded
```

- 通配符 `"*"` 订阅所有
- 内置 JSONL 日志 + Webhook 投递
- per-handler channel (buffer 256), 满时 drop + warn

<!-- last-modified: 2026-05-13 -->
