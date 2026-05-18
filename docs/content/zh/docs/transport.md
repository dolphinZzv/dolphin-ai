---
title: 传输协议
description: 通过不同传输协议连接 dolphin
weight: 15
---

dolphin 支持五种传输协议，可以在配置文件中任意组合启用。

## stdio

默认传输协议，直接在终端运行。

```bash
./dolphin
```

首次运行会引导你设置职业画像和推荐工具。

## SSH

通过 SSH 远程连接。

```bash
ssh dolphin@host -p 2222
```

与 stdio 使用相同的 agent 会话和终端界面，但支持远程访问。

**配置：**

```yaml
transport:
  ssh:
    enabled: true
    listen: ":2222"
    host_key: ~/.dolphin/ssh_host_key
```

## MQTT

轻量级发布/订阅消息协议，适合嵌入式设备、聊天应用或事件驱动自动化。

```yaml
transport:
  mqtt:
    enabled: true
    broker: tcp://broker.example.com:1883
    topic_prefix: dolphin
    client_id: dolphin-agent
```

MQTT 附带原生 macOS 客户端（Panda）。

## Email

把命令作为邮件主题发送，通过 IMAP 轮询接收回复。

```yaml
transport:
  email:
    enabled: true
    imap_server: imap.example.com
    imap_port: 993
    smtp_server: smtp.example.com
    smtp_port: 587
    username: dolphin@example.com
    password: your-email-password
    allowed_senders:
      - you@example.com
    poll_interval: 30s
```

只处理 `allowed_senders` 列表中的发件人。回复使用正确的邮件线程（In-Reply-To/References 头）。

## DingTalk

通过钉钉机器人接入，支持团队协作。基于钉钉 Stream 模式实现交互式命令和通知推送。

```yaml
transport:
  dingtalk:
    enabled: true
    client_id: your-client-id
    client_secret: your-client-secret
```

需要创建钉钉机器人应用。使用 Stream 模式实现实时双向通信。
