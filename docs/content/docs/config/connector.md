---
title: 连接器配置
weight: 2
---

## 钉钉

使用钉钉 Stream Mode 协议接入：

```yaml
dingtalk:
  enabled: true
  client_id: your-client-id
  client_secret: your-client-secret
  webhook_url: "https://oapi.dingtalk.com/robot/send?access_token=xxx"
  allow_users:
    - "user1"
```

`allow_users` 支持 glob 通配符，配置后仅允许指定用户昵称使用（未配置时允许所有用户）。

## 企业微信

使用企业微信智能机器人 WebSocket 协议接入：

```yaml
wework:
  enabled: true
  bot_id: your-bot-id
  bot_secret: your-bot-secret
  allow_users:
    - "user1"
```

`allow_users` 支持 glob 通配符，配置后仅允许指定用户 ID 使用（未配置时拒绝所有用户）。

## 邮件

通过 IMAP 接收邮件、SMTP 发送邮件：

```yaml
email:
  enabled: true
  address: "bot@example.com"
  password: your-password
  imap_server: imap.exmail.qq.com
  imap_port: "993"
  smtp_server: smtp.exmail.qq.com
  smtp_port: "465"
  allow_senders:
    - "user@example.com"
```

`allow_senders` 支持 glob 通配符，未配置时拒绝所有发件人。

## 标准 I/O

终端交互（无需额外配置，自动启用）：

```yaml
transport:
  - type: stdio
```
