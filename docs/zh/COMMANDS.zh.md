# 内部命令

dolphin 提供内置的斜杠命令，在所有传输层（终端、SSH、邮件、MQTT、钉钉）中均可使用。在会话中输入 `/help` 可查看完整列表。

## 会话管理

| 命令 | 说明 |
|------|------|
| `/exit` 或 `exit` 或 `quit` | 退出 agent |
| `/new` | 开始新会话（旧会话会先生成摘要） |
| `/status` | 显示当前会话和 agent 状态 |
| `/cancel [id]` | 取消所有正在运行的任务，或取消指定 ID 的任务 |
| `/reload` | 重新加载（重启）agent |

## 信息查询

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助信息 |
| `/mcp` | 列出所有已注册的 MCP 工具及说明 |
| `/agents [name]` | 列出 agent 及其状态；指定名称可查看详情 |
| `/skills [sub]` | 列出可用技能。子命令：`new`、`delete`、`show` |
| `/commands [sub]` | 列出用户自定义命令。子命令：`new`、`delete`、`show` |
| `/workflow [sub]` | 列出可用工作流。子命令：`new`、`delete`、`show` |
| `/sessions [sub]` | 列出历史会话。子命令：`dump <id>` |
| `/context [sub]` | 显示上下文摘要。子命令：`system`、`current`、`<section>` |
| `/transport` | 显示已启用的传输层 |

## 配置管理

| 命令 | 说明 |
|------|------|
| `/config [sub]` | 查看或修改配置。子命令：`get`、`set` |
| `/model [name]` | 列出或切换 LLM 模型 |
| `/provider [sub]` | 列出或切换 LLM 提供商。子命令：`switch [name]` |

## 任务管理

| 命令 | 说明 |
|------|------|
| `/crontab` | 查看已计划的 cron 任务 |

## 其它

| 命令 | 说明 |
|------|------|
| `/forget <name>` | 重置某个 agent 的对话上下文 |
| `/feedback` | 通过邮件向开发团队发送反馈 |

## 使用说明

- 子命令的使用方式为 `/command subcommand [args]`，例如：`/skills new`
- 使用 `/help <command>` 查看具体命令的详细用法
- 所有命令在所有传输模式中均可使用 —— 终端、SSH、邮件、MQTT、钉钉
- 用户自定义命令（来自 `.dolphin/commands/`）同样使用 `/` 前缀调用
