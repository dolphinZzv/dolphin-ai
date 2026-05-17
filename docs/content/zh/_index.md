---
description: 一个在你工作环境中无处不在的 AI Agent — 终端、邮件、聊天或 SSH
---

小海豚是一个跨平台 AI Agent，可以执行 Shell 命令、控制浏览器、调度子 Agent 并行工作、按定时任务自动运行。它不关心你从哪个入口联系它——本地终端、SSH、MQTT 还是邮件，同一 Agent 随时待命。

## 为什么用小海豚？

大多数 AI 编程工具将你锁定在特定的编辑器或 Web UI 中。对于写代码来说这没问题，但实际工作远不止于此。你可能想在手机上通过邮件向 Agent 提问，或者让它每晚自动执行定时任务，又或者 SSH 到服务器让 Agent 诊断问题。

小海豚不关心你从哪个门进来——它一视同仁。同一 Agent、同一套工具、同一份会话状态，无论传输方式。

## 快速开始

选择你的服务商，设置完整环境变量：

```bash
# DeepSeek 示例
export DZ_LLM_TYPE="openai"
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_BASE_URL="https://api.deepseek.com/v1"
export DZ_LLM_MODEL="deepseek-v4-flash"

# 运行 dolphin
./dolphin
```

首次运行时，小海豚会引导你完成设置向导——选择角色、生成配置文件和系统提示文件。详见[快速开始指南](docs/quickstart/)。

## 核心特性

- **多传输层**：终端、SSH、MQTT、邮件 — 同一 Agent 无处不在
- **工具丰富**：Shell 命令、浏览器自动化（CDP）、MCP 工具、Webhook
- **多 Agent 协作**：并行子 Agent 执行复杂工作流
- **技能系统**：通过 Markdown 文件按需加载技能
- **定时任务**：CRONTAB.md 文件定义周期性任务
- **会话持久化**：自动检查点、摘要、日记聚合
- **可扩展**：插件系统，支持 Hook 和 Event

## 分发渠道

- [GitHub](https://github.com/dolphinZzv/dolphin)
- [Gitee](https://gitee.com/dolphinzzv/dolphindolphin)

<!-- last-modified: 2026-05-14 -->
