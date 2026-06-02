---
title: 调用限额
weight: 5
---

对 LLM 调用设置日配额，支持请求次数和 Token 消耗两种指标。

## 全局限额

```yaml
llm:
  limit:
    max_requests:
      hard: 1000
      soft: 800
    max_total_tokens:
      hard: 10000000
      soft: 8000000
    max_input_tokens:
      hard: 8000000
    max_output_tokens:
      hard: 2000000
    reset_cron: "0 0 * * *"
```

## 单模型限额

在模型定义中加 `limit` 字段：

```yaml
llm:
  deepseek_anthropic:
    models:
      - name: deepseek-v4-pro
        limit:
          max_daily_requests: 100
      - name: deepseek-v4-flash
```

不配 `limit` 表示该模型不单独限制，仅受全局限额约束。

## 字段说明

### `llm.limit`

| 字段 | 类型 | 说明 |
|------|------|------|
| `max_requests` | `{hard, soft}` 或整数 | 周期内最大请求次数（受 reset_cron 重置） |
| `max_total_tokens` | `{hard, soft}` 或整数 | 累计总 Token 消耗（不受 reset_cron 重置） |
| `max_input_tokens` | `{hard, soft}` 或整数 | 周期内输入 Token 限额（受 reset_cron 重置） |
| `max_output_tokens` | `{hard, soft}` 或整数 | 周期内输出 Token 限额（受 reset_cron 重置） |
| `reset_cron` | string | Cron 表达式，定时清零计数器（如 `"0 0 * * *"` 每日零点） |

### Hard / Soft

每个限额项支持两种阈值：

| 类型 | 效果 |
|------|------|
| **hard** | 超过后阻断 LLM 调用，返回错误提示 |
| **soft** | 超过后发送 webhook 告警，但 LLM 调用继续 |

值可以是整数（等价于 hard），或对象 `{hard: 1000, soft: 800}`。

未配置 soft 时，默认 = hard × 80%。

### `agent.webhook`

```yaml
agent:
  webhook:
    url: "https://hooks.example.com/alert"
```

soft 和 hard 限额触发时，会向该地址发送 POST 请求。请求体为 JSON：

```json
{
  "type": "limit.soft_warn",
  "session_id": "...",
  "timestamp": "2026-06-02T10:00:00Z",
  "payload": {
    "metric": "max_daily_requests",
    "current": 850,
    "soft": 800,
    "hard": 1000,
    "model": "deepseek-v4-flash"
  }
}
```

## 说明

- 计数器存储在 `.dolphin/limits/counters.json`，重启不丢失
- 进程在重置期间未运行时，启动后自动执行过期重置
- `reset_cron` 使用标准 5 位 Cron 表达式
- `max_total_tokens` 为累计限额，不受 `reset_cron` 影响，永不重置
