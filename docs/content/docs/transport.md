---
title: 传输层
---

Dolphin 支持多种传输层，可在 `config.yaml` 中配置。

## DingTalk（钉钉）

使用钉钉 Stream Mode 接入：

```yaml
transport:
  - type: dingtalk
    dingtalk:
      client_id: your-client-id
      client_secret: your-client-secret
      agent_name: 机器人名称
```

## WeWork（企业微信）

使用企业微信智能机器人 WebSocket 接入：

```yaml
transport:
  - type: wework
    wework:
      bot_id: your-bot-id
      bot_secret: your-bot-secret
      allow_users: user1,user2
```

## Email（邮件）

通过 IMAP/SMTP 接入：

```yaml
transport:
  - type: email
    email:
      imap_server: imap.exmail.qq.com
      smtp_server: smtp.exmail.qq.com
      email_address: bot@example.com
      password: your-password
      allow_senders: user@example.com
```

## Stdio（标准输入输出）

本地终端交互：

```yaml
transport:
  - type: stdio
```
