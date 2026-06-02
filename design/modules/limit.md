# Limit

LLM 调用配额管理模块。在 Agent Loop 执行 LLM 调用前拦截检查，在调用完成后记录使用量。

## 核心思路

使用 Hook 的同步性做预检拦截，EventBus 做异步记录：

| 机制 | 特点 | 用途 |
|---|---|---|
| **Hook Registry** | 同步，可返回 error | 请求前检查配额，超限则阻断 |
| **EventBus** | 异步，发后即忘 | 请求后记录使用量；触发 webhook 告警 |

## Hard / Soft 策略

| 类型 | 超过阈值时 |
|---|---|
| **hard** | 阻断 LLM 调用 + 发 webhook 告警 |
| **soft** | 日志警告 + 发 webhook 告警，不阻断调用 |

soft 未显式配置时默认 = hard × 80%。

## 工作流程

```
LLMStage.Process()
  ├─ HookReg.DispatchCheck(EventCheckLLM)
  │    └─ LimitHook.Handle()
  │         ├─ 超过 hard → EventBus.Publish(EventLimitHardBlock) → return error → 阻断
  │         ├─ 超过 soft → EventBus.Publish(EventLimitSoftWarn) → log → 继续
  │         └─ 全部通过 → return nil → 继续
  ├─ Provider.CompleteStream()
  ├─ EventBus.Publish(EventLLMComplete)
  │    └─ Limiter 记录 + WebhookNotifier 忽略
  └─

EventBus (异步):
  ├─ EventLimitSoftWarn → WebhookNotifier → HTTP POST → webhook URL
  ├─ EventLimitHardBlock → WebhookNotifier → HTTP POST → webhook URL
```

## 配置

全局 limit 和单 model limit 均在 `llm` 下配置：

```yaml
llm:
  limit:
    reset_cron: "0 0 * * *"
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

  deepseek_anthropic:
    models:
      - name: deepseek-v4-pro
        limit:
          max_daily_requests: 100

agent:
  webhook:
    url: "https://hooks.example.com/alert"
```

## Store

```go
type Store interface {
    Get(key string) (int64, error)
    Increment(key string, delta int64) (int64, error)
    Reset(prefix string) error
    GetAll() (map[string]int64, error)
}
```

| 实现 | 用途 |
|---|---|
| MemoryStore | 单元测试 |
| FileStore | 生产环境，JSON 文件持久化 |

FileStore 保存 `last_reset` 时间戳，启动时检测过期自动重置。

## 重置

使用 `robfig/cron` 定时执行 `llm.limit.reset_cron`。进程重启时通过 `last_reset` 检测是否错过重置。

## Event 类型

| 事件 | 说明 |
|---|---|
| `limit.check.llm` | Hook 预检事件 |
| `limit.soft_warn` | soft 超限，不阻断 |
| `limit.hard_block` | hard 超限，已阻断 |

<!-- last-modified: 2026-06-02 -->
