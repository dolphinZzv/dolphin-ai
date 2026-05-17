# Directory Structure

```
dolphin/
├── main.go                       # 入口
├── go.mod / go.sum
├── Makefile / Dockerfile
├── AGENTS.md / WORKFLOW.md
├── docs/
│   ├── en/
│   │   ├── INSTALL.md
│   │   └── config.example.yaml
│   └── zh/
│       ├── INSTALL.zh.md
│       └── config.example.zh.yaml
├── cmd/
│   ├── root.go                   # 根命令 + runAgent() 启动编排
│   ├── setup.go                  # 职业引导
│   ├── init.go                   # 生成默认配置
│   ├── reset.go                  # 清除运行时数据
│   ├── new.go                    # reset + 新会话
│   └── update.go                 # 二进制更新
├── internal/
│   ├── agent/                    # 核心智能体
│   │   ├── llm.go                # Provider 接口 + 类型定义
│   │   ├── loop.go               # Agent.Run() + runTurn() + RunTask()
│   │   ├── context.go            # ContextBuilder wrapper
│   │   ├── provider_openai.go
│   │   ├── provider_anthropic.go
│   │   ├── compressor.go         # 接口 + DropCompressor
│   │   ├── compressor_segment.go / _tiered.go / _incremental.go / _topic.go
│   │   ├── coordinator.go        # 多 Agent 调度器
│   │   ├── agent_pool.go         # AgentPool + AgentInstance
│   │   ├── agent_def.go          # AgentDef YAML 加载
│   │   ├── agent_types.go
│   │   ├── channel_io.go         # 子 Agent 内存 UserIO
│   │   ├── config_handler.go     # 运行时配置 MCP 工具
│   │   └── metrics.go            # Agent 级 Prometheus 指标
│   ├── config/                   # 配置管理
│   │   ├── config.go             # Config + Load() + Validate()
│   │   ├── config_gen.go         # 配置文件生成
│   │   ├── career.go             # 首次运行引导
│   │   ├── project.go            # 项目检测
│   │   ├── repo.go               # Manifest 仓库
│   │   └── recommend.go          # 工具推荐
│   ├── context/                  # 系统提示构建
│   │   ├── builder.go            # 多目录 fallback + 缓存
│   │   └── preface.go            # go:embed PREFACE.md + BUILTIN_SKILLS.md
│   ├── transport/                # 传输层
│   │   ├── transport.go          # Transport + UserIO 接口
│   │   ├── stdio.go / ssh.go / mqtt.go / email.go
│   │   └── metrics.go
│   ├── mcp/                      # MCP 工具系统
│   │   ├── registry.go           # 注册 + 执行 + 统计 + Clone + 渐进披露
│   │   ├── shell.go / cdp.go / email.go / webhook.go
│   │   ├── client.go             # 外部 MCP 服务器连接
│   │   └── transport_stdio.go / _sse.go / _http_stream.go
│   ├── session/                  # 会话管理
│   │   ├── session.go            # Session + Manager + JSONL + Reaper
│   │   └── summary.go
│   ├── skill/                    # 技能系统
│   │   └── skill.go
│   ├── command/                  # 用户命令
│   │   └── command.go
│   ├── scheduler/                # 定时任务
│   │   └── crontab.go
│   ├── event/                    # 异步事件总线
│   │   └── event.go
│   ├── hook/                     # 同步钩子系统
│   │   └── hook.go
│   ├── plugin/                   # 插件系统
│   │   ├── plugin.go / manager.go / scripts.go
│   ├── diary/                    # 会话日历
│   │   ├── diary.go / prune.go
│   ├── metrics/                  # Prometheus 注册表
│   │   ├── metrics.go / http.go
│   ├── logger/                   # 结构化日志
│   │   └── logger.go
│   └── i18n/                     # 国际化
│       ├── i18n.go / en.go / zh.go
├── design/                       # 架构文档
└── .dolphin/                     # 项目级配置
    ├── config.yaml
    ├── AGENTS.md / RULES.md / USER.md / SYSTEM.md
    ├── skills/ / commands/ / agents/
```

<!-- last-modified: 2026-05-13 -->
