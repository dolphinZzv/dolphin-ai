---
title: 快速开始
weight: 1
---

## 安装

下载对应平台的二进制文件，或通过 Go 安装：

```shell
go install github.com/dolphinZzv/dolphin-ai/cmd/dolphin@latest
```

## 配置

创建 `config.yaml`，以钉钉为例：

```yaml
llm:
  api_key: sk-xxx
  model: gpt-4o

transport:
  - type: dingtalk
    dingtalk:
      client_id: your-client-id
      client_secret: your-client-secret
```

更多传输层配置请参阅[配置指南](./config/)。

## 运行

```shell
./dolphin -c config.yaml
```

现在你可以直接在钉钉中和 Dolphin 对话了。

## 命令

在对话中输入 `/help` 查看所有可用命令，或参阅[命令参考](./commands/)。

## 支持的传输层

| 传输层 | 说明 |
|--------|------|
| 钉钉 | 通过钉钉 Stream Mode 接入，直接在群聊中使用 |
| 企业微信 | 通过企业微信智能机器人接入 |
| 邮件 | 通过 IMAP/SMTP 接入，邮件往来即对话 |
| 标准 I/O | 终端直接交互，适合调试 |

## 支持的模型

| 提供商 | 说明 |
|--------|------|
| OpenAI | GPT 系列模型 |
| Anthropic | Claude 系列模型 |
| DeepSeek | DeepSeek V4 系列模型（支持 OpenAI / Anthropic 双协议） |
