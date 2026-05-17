# Configuration (`internal/config/`)

## Loading

四级 Viper 合并，后加载覆盖先加载：

1. Hardcoded defaults → 2. `/etc/dolphin/config.yaml` → 3. `~/.dolphin/config.yaml` → 4. `./.dolphin/config.yaml` → 5. `-c` flag

环境变量前缀 `DZ_` 完全加载后覆盖。

## Config Structure

| Section | Struct | Key Fields |
|---------|--------|------------|
| `llm` | `LLMConfig` | Type, BaseURL, APIKey, Model, MaxTokens, Providers[], Temperature, MaxContextTokens, CompressMode |
| `session` | `SessionConfig` | MaxLoop, Summary, MaxAge, Resume |
| `transport` | `TransportConfig` | Stdio/SSH/MQTT/Email 四子结构 |
| `mcp` | `MCPConfig` | Shell/CDP/Email/Webhook 开关, Servers[], Repos[] |
| `agent_pool` | `PoolConfig` | MaxConcurrency, DefaultTimeout, WorkspaceDir, IdleTimeout |
| `skills` | `SkillsConfig` | Dir, Repos[] |
| `crontab` | `CrontabConfig` | File, CheckInterval |
| `pprof` / `metrics` | `PprofConfig` / `MetricsConfig` | Enabled, Addr |
| `diary` | `DiaryConfig` | Dir, MaxDaySessions, MaxTotalMB |
| `plugins` | `PluginsConfig` | Enabled, Dir, WebhookURL, HeartbeatTurns |

## First-Run Flow

`career.go` + `project.go` + `repo.go` + `recommend.go`:

1. 职业领域选择 → Profile 匹配内置工具映射
2. 从 Repos (社区 manifest 仓库) 增补工具
3. 异步 Project 检测 (读 go.mod, package.json, Cargo.toml 等)
4. 生成 `SYSTEM.md` + 配置文件
5. `~/.dolphin/.first-run-done` 标记

## Runtime Config

`config_handler.go` — 通过 MCP 工具提供运行时的 `config list` / `config get` / `config set` / `config save`，支持修改 temperature、模型、压缩模式等。

<!-- last-modified: 2026-05-16 -->
