# 传输层

Dolphin 支持多种传输协议作为 Agent 通信方式。

| 传输层 | 协议 | 默认端口 | 用途 |
|--------|------|:--------:|------|
| [Stdio](stdio.md) | stdin/stdout | — | 本地命令行、REPL 模式 |
| [SSH](ssh.md) | SSH shell | `:2222` | 远程终端访问 |
| [MQTT](mqtt.md) | MQTT pub/sub | 取决于配置 | IoT、消息队列集成 |
| [Email](email.md) | SMTP/IMAP/POP3 | 取决于配置 | 邮件交互 |
| [DingTalk](dingtalk.md) | 钉钉机器人 | — | 钉钉集成 |
| [A2A](a2a.md) | JSON-RPC (Google) | `:8334` | Agent 间通信 (A2A 协议) |

所有传输层都实现了统一的 `UserIO` 接口（`ReadLine` / `Write` / `Flush`），因此协调器可以统一处理。
