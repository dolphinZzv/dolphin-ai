# Transports

Dolphin supports multiple transport protocols for agent communication.

| Transport | Protocol | Default Port | Use Case |
|-----------|----------|-------------|----------|
| [STDIO](stdio.md) | stdin/stdout | — | Local CLI, REPL mode |
| [SSH](ssh.md) | SSH shell | `:2222` | Remote terminal access |
| [MQTT](mqtt.md) | MQTT pub/sub | varies | IoT, message queue integration |
| [Email](email.md) | SMTP/IMAP/POP3 | varies | Email-based agent interaction |
| [钉钉](dingtalk.md) | DingTalk Webhook | — | Alibaba DingTalk integration |
| [A2A](a2a.md) | JSON-RPC (Google) | `:8334` | Inter-agent communication (A2A) |

Each transport implements a common `UserIO` interface — `ReadLine` / `Write` / `Flush` — which allows the coordinator to handle all transports uniformly.
