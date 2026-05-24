# 钉钉 DingTalk Transport

The DingTalk transport integrates Dolphin with [Alibaba DingTalk](https://www.dingtalk.com/), a popular enterprise communication platform. It connects via DingTalk's bot/webhook API.

## Configuration

```yaml
transport:
  dingtalk:
    enabled: false
    client_id: ""              # AppKey from DingTalk Open Platform
    client_secret: ""          # AppSecret from DingTalk Open Platform
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable DingTalk transport |
| `client_id` | string | `""` | AppKey from DingTalk Open Platform |
| `client_secret` | string | `""` | AppSecret from DingTalk Open Platform |

## Setup

1. Create a bot application on the [DingTalk Open Platform](https://open-dev.dingtalk.com/)
2. Enable **Messaging API** in the bot settings
3. Get the **AppKey** (`client_id`) and **AppSecret** (`client_secret`)
4. Configure them in `transport.dingtalk`

## Usage

Once configured and enabled, Dolphin will respond to messages sent to the DingTalk bot.

## Related Links

- [DingTalk Open Platform Console](https://open-dev.dingtalk.com/)
- [DingTalk Bot API Documentation](https://open.dingtalk.com/document/orgapp/robot-overview)
- [DingTalk AppKey & AppSecret Guide](https://open.dingtalk.com/document/orgapp/create-an-application)

## See Also

- [A2A Transport](a2a.md) — Google Agent-to-Agent protocol

---

> Last modified: 2026-05-22
