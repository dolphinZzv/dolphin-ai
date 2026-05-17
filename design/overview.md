# Overall Architecture

## Context

Go 实现的 AI Agent 系统，支持 stdio/SSH/MQTT/Email 四种传输层，具备 Agent Loop、Viper 多级配置、MCP 工具集、会话管理、多 Agent 协同、插件系统、Prometheus 可观测性、国际化、定时任务和 Skills 技能系统。

## v0.1 — Single Agent

```mermaid
flowchart TB
    subgraph Entry
        main["main.go"]
        cmd["cmd/root.go (Cobra)"]
    end
    subgraph Config
        config["config.Load() — Viper 四级加载"]
        career["首次运行: 职业引导 + 工具推荐"]
    end
    subgraph Context
        preface["PREFACE.md (go:embed)"]
        builtinSkills["BUILTIN_SKILLS.md"]
        builder["context.Builder"]
    end
    subgraph Transport
        stdio["stdio"]; ssh["SSH"]; mqtt["MQTT"]; email["Email"]
    end
    subgraph Session
        sessionMgr["session.NewManager()"]
        jsonl["{dir}/{id}.jsonl"]
        reaper["Reaper"]
    end
    subgraph MCP
        registry["mcp.Registry"]
        shell["shell"]; cdp["cdp"]; emailTool["email"]; webhook["webhook"]
        ext["外部 MCP Servers"]
    end
    subgraph AgentLoop
        buildPrompt["构建 System Prompt"]
        callLLM["Call LLM (OpenAI / Anthropic)"]
        parseTools["解析 Tool Calls"]
        execTools["串行执行工具"]
        compress["上下文压缩"]
        fillBack["回填 Tool Results"]
    end
    main --> cmd --> config & builder & Transport & sessionMgr & registry
    builder --> preface & builtinSkills
    registry --> shell & cdp & emailTool & webhook & ext
    Transport --> AgentLoop
    AgentLoop --> buildPrompt --> callLLM --> parseTools --> execTools --> compress --> fillBack
```

## v0.2 — Multi-Agent

```mermaid
flowchart TB
    subgraph Startup
        agentsDir[".dolphin/agents/ 存在?"]
        yes["是 → Coordinator 模式"]; no["否 → 单 Agent 模式"]
    end
    subgraph Coordinator["Coordinator (per-connection)"]
        coordAgent["Agent (克隆 toolReg)"]
        coordPool["AgentPool"]
        coordTools["dispatch_task / create_agent / cancel_task"]
    end
    subgraph SubAgents["Sub-Agent goroutine 池"]
        A1["reviewer (持久化)"]
        A2["sysadmin (持久化)"]
        A3["query-analyzer (临时)"]
    end
    agentsDir --> yes & no
    yes --> Coordinator --> coordPool --> SubAgents
```

## v0.3 — Full System

```mermaid
flowchart TB
    subgraph Init
        loadConfig["config.Load()"]
        firstRun["首次引导"]; logger["zap Init"]
        sessMgr["session.NewManager()"]
        toolReg["mcp.NewRegistry()"]
        regTools["内置工具(shell/cdp/email/webhook)"]
        extServers["外部 MCP Servers"]
        plugins["plugin + hook + event"]
        skills["skill.NewManager()"]
        commands["command.NewManager()"]
        cron["scheduler.NewManager()"]
    end
    subgraph Actors["Actor 组 (oklog/run)"]
        signal["SIGINT/SIGTERM"]; reaper["Reaper"]
        diarySync["Diary 同步"]
        transports["stdio / SSH / MQTT / Email"]
        pprof["pprof"]; metricsSrv["Metrics HTTP"]
    end
    subgraph PerConn
        coord["Coordinator.Run()"]
        agent["Agent (LLM + Session)"]
        subAgents["AgentPool"]
        cronTasks["cron dueCh"]
    end
    Init --> Actors --> PerConn
```

## Startup Flow

```
runAgent():
  1. config.Load() — 四级 Viper + 环境变量覆盖
  2. logger.Init() — zap + lumberjack
  3. 首次运行: 职业引导 → 工具推荐 → SYSTEM.md → config gen
  4. session.NewManager()
  5. mcp.NewRegistry() → Register 内置工具 → LoadServers()
  6. 检查 .dolphin/agents/ → coordinator / single-agent
  7. skill + command + scheduler Manager
  8. hook + event + plugin Manager
  9. newCoordinator factory (per-connection)
  10. run.Group{ signal, reaper, diary, transports, pprof, metrics }
  11. g.Run() — 任一 Actor 退出 → cancel 全部
```

<!-- last-modified: 2026-05-13 -->
