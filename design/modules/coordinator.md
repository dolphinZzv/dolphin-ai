# Multi-Agent Coordination (`internal/agent/` — v0.2)

## Architecture

```mermaid
flowchart TB
    subgraph Coordinator["Coordinator (per-connection)"]
        A["Agent<br/>(LLM + Session + cloned Registry)"]
        P["AgentPool"]
    end

    subgraph Pool["AgentPool"]
        R1["reviewer<br/>(持久化)"]
        R2["sysadmin<br/>(持久化)"]
        T1["temp-xxx<br/>(临时)"]
        T2["temp-yyy<br/>(临时)"]
    end

    subgraph Background["Background Tasks"]
        Cron["cron dueCh listener"]
    end

    Coordinator --> A
    Coordinator --> P
    P --> R1
    P --> R2
    P --> T1
    P --> T2
    Coordinator --> Cron

    style Coordinator fill:#e8f5e9
    style Pool fill:#fff3e0
```

## AgentInstance State Machine

```mermaid
stateDiagram-v2
    [*] --> idle: 创建
    idle --> busy: dispatch_task
    busy --> completed: 任务成功
    busy --> error: 任务失败
    busy --> cancelled: cancel_task
    busy --> timeout: 超时
    completed --> idle: 归还池
    error --> idle: 归还池
    cancelled --> idle: 归还池
    timeout --> idle: 归还池
    idle --> [*]: delete_agent
```

## Task Dispatch Flow

```mermaid
sequenceDiagram
    participant C as Coordinator
    participant P as AgentPool
    participant A as AgentInstance

    C->>C: dispatch_task(name, input, timeout)
    C->>P: SubmitTask(task)
    P->>A: assign(task)
    A->>A: 执行任务
    A-->>P: result
    P-->>C: TaskResult
    C->>C: 合成响应

    Note over P: Collect() 非阻塞<br/>批量收集已完成结果
```

## Coordinator MCP Tools

| Tool | Description |
|------|-------------|
| `dispatch_task` | 分发任务给池中 Agent |
| `create_agent` | 创建持久化 Agent |
| `get_agent_status` | 查看 Agent 状态 |
| `cancel_task` | 取消运行中任务 |
| `delete_agent` | 删除持久化 Agent |

## ChannelIO

`agent/channel_io.go` — 内存 UserIO 实现，通过 channel 在 Coordinator 与子 Agent 间通信。

```mermaid
flowchart LR
    subgraph Coordinator
        Out["chan UserOutput"]
        In["chan UserInput"]
    end

    subgraph ChannelIO
        O["UserOutput → coordinator io.WriteLine()"]
        I["coordinator io.ReadLine() → UserInput"]
    end

    Out --> O
    I --> In
```
