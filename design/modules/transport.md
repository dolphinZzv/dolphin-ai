# Transport Layer (`internal/transport/`)

## Interfaces

```go
type Transport interface {
    Name() string
    Start(ctx) error   // 阻塞直到会话结束
    Close() error
}

type UserIO interface {
    ReadLine() (string, error)
    WriteLine(string) error
    WriteString(string) error
    Capabilities() Capabilities
    Context() context.Context
}
```

每个 Transport 绑定一个独立的 Coordinator goroutine，各自维护独立的 Session。

## Capabilities

```go
type Capabilities struct {
    Streaming       bool  // 流式输出（边生成边发送）
    Flushable       bool  // 支持 Flush 语义
    ConfirmExit     bool  // 退出前需用户确认
    ShowToolDetails bool  // 展示工具调用参数/结果细节
}
```

| Flag | stdio | SSH | Email | DingTalk | MQTT | ACP | A2A |
|------|-------|-----|-------|----------|------|-----|-----|
| Streaming | true | true | false | false | false | false | false |
| ShowToolDetails | true | true | false | false | false | false | false |

## Session Resume

```
启动 coordinator
  ├─ ShowToolDetails == true (stdio/SSH)
  │   └─ resume 配置 + 提示确认 → 恢复/新建
  └─ ShowToolDetails == false (email/DingTalk/MQTT)
      └─ 始终自动恢复最后一次 session（持续记忆）
```

非交互 transport 用户发 `/new` 才会清空记忆开启全新 session。

## Slash Commands

| Command | Action |
|---------|--------|
| `/exit` | 退出 session |
| `/help` | 显示帮助 |
| `/status` | 会话状态 |
| `/mcp` | 列出 MCP 工具 |
| `/new` | 持久化当前会话摘要 → 开新 session 清空记忆 |
| `/skills` | 列出技能 |

## Implementations

| Transport | Library | Mechanism | Capabilities |
|-----------|---------|-----------|-------------|
| **stdio** | `chzyer/readline` | stdin/stdout 行编辑 | 全部支持 |
| **SSH** | `golang.org/x/crypto/ssh` | TCP :2222, 密码认证 | 全部支持 |
| **MQTT** | `eclipse/paho.mqtt.golang` | Subscribe command topic, Publish response topic | 非流式 |
| **Email** | `net/smtp` + `emersion/go-imap` | SMTP 发送, IMAP 轮询, 正文 → 命令, In-Reply-To 线索化回复 | 非流式 |
| **DingTalk** | `open-dingtalk/dingtalk-stream-sdk-go` | Stream 模式 (WebSocket 长连接), 无需公网IP | 非流式 |
| **ACP** | `net/http` | REST API 端点，IBM BeeAI ACP 规范 | 非流式 |
| **A2A** | `net/http` | JSON-RPC 2.0 端点，Google A2A 规范 | 非流式 |

## Email Transport Flow

```mermaid
sequenceDiagram
    participant U as User
    participant IMAP as IMAP Server
    participant Agent as dolphin Agent
    participant SMTP as SMTP Server

    U->>SMTP: Send email to bot
    SMTP->>IMAP: Deliver to INBOX

    loop every poll_interval (10s)
        Agent->>IMAP: TLS connect + login
        Agent->>IMAP: SEARCH UNSEEN
        IMAP->>Agent: unseen seq numbers
        Agent->>IMAP: STORE +Flags \Seen (mark read)
        Agent->>IMAP: FETCH latest (Envelope + BODY[TEXT])
        IMAP->>Agent: subject, body, Message-Id, from

        alt self-sent or before startTime
            Agent->>Agent: skip (isOwnAddress / startTime)
        else allowed_senders check
            Agent->>Agent: skip if sender not in whitelist
        else valid message
            Agent->>Agent: decode subject (RFC 2047)
            Agent->>Agent: store lastSender, lastMsgID, lastSubject
            Agent->>Agent: post body (or subject) to msgCh
            Agent->>Agent: LLM processes command
            Agent->>SMTP: sendMail with In-Reply-To / References
            SMTP->>U: threaded reply (Re: original subject)
        end
    end
```

### Filter Chain

1. **startTime check** — skip messages dated before agent process started
2. **isOwnAddress** — skip self-sent messages (from == `cfg.From` or `cfg.Username`, supports `@domain` suffix matching)
3. **allowed_senders** — if configured, only process messages from allowlisted addresses/domains
4. **Subject decode** — RFC 2047 encoded subjects (GBK, UTF-8 B/Q) are decoded before processing

### Reply Headers

| Header | Source |
|--------|--------|
| `From` | `cfg.From` (fallback: `cfg.Username`) |
| `To` | `lastSender` (set from incoming message envelope) |
| `Subject` | `Re: <decoded original subject>` |
| `In-Reply-To` | `<original Message-Id>` |
| `References` | `<original Message-Id>` |

> Last modified: 2026-05-17
