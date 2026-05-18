---
title: Transport
description: Connect to dolphin via different transports
weight: 15
---

dolphin supports five transports. You can enable any combination of them in the config file.

## stdio

The default transport. Runs in your terminal.

```bash
./dolphin
```

First run walks you through setting up your profile and recommended tools.

## SSH

Connect from anywhere via SSH.

```bash
ssh dolphin@host -p 2222
```

The same agent session and terminal interface as stdio, but remote.

**Configuration:**

```yaml
transport:
  ssh:
    enabled: true
    listen: ":2222"
    host_key: ~/.dolphin/ssh_host_key
```

## MQTT

Lightweight pub/sub messaging for embedded devices, chat apps, or event-driven automation.

```yaml
transport:
  mqtt:
    enabled: true
    broker: tcp://broker.example.com:1883
    topic_prefix: dolphin
    client_id: dolphin-agent
```

MQTT ships with a native macOS client (Panda).

## Email

Send a command as an email subject, get the response back via IMAP polling.

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

Only emails from `allowed_senders` are processed. Replies use proper threading (In-Reply-To/References headers).

## DingTalk

Connect through DingTalk bot for team collaboration. Supports interactive commands and notifications via DingTalk Stream mode.

```yaml
transport:
  dingtalk:
    enabled: true
    client_id: your-client-id
    client_secret: your-client-secret
```

Requires a DingTalk bot application. Uses Stream mode for real-time bidirectional communication.
