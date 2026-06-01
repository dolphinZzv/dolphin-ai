---
title: Dolphin AI Agent
---

Dolphin 支持 DingTalk、WeWork、Email、Stdio 等多种传输层协议，提供统一的 LLM 交互入口。

### 核心特性

- **多传输层支持**：同时接入钉钉、企业微信、邮件、终端等多种消息渠道
- **多 LLM 提供商**：内置 OpenAI、Anthropic、DeepSeek 等主流模型，支持运行时动态切换
- **可扩展工具系统**：基于 MCP 协议的插件化工具架构，支持动态加载
- **会话管理**：多会话隔离，跨传输层无缝切换
- **可观测性**：集成 OpenTelemetry，支持链路追踪和结构化日志
