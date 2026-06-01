---
title: 模型配置
weight: 1
---

以 DeepSeek 为例，配置 LLM 模型。

## 单模型

```yaml
llm:
  provider: anthropic
  model: deepseek-v4-flash
  api_key: sk-xxx
  api_type: openai
  base_url: https://api.deepseek.com
  temperature: 0.7
  max_tokens: 4096
  timeout: 30s
```

### 字段说明

| 字段 | 说明 |
|------|------|
| `api_key` | API 密钥 |
| `model` | 模型名称 |
| `api_type` | 协议类型：`openai` 或 `anthropic` |
| `base_url` | API 端点地址（可选） |
| `provider` | 厂商名称（可选） |
| `temperature` | 采样温度（可选，默认 0.7） |
| `max_tokens` | 最大输出 Token 数（可选） |
| `max_retries` | 失败重试次数（可选，默认 3） |
| `timeout` | 请求超时时间（可选，默认 30s） |

> `api_type` 决定协议格式。DeepSeek 同时支持 `openai` 和 `anthropic` 两种协议。
> `provider` 用于厂商区分，格式为 `vendor/api_type` 的两层查找机制。

## 多模型切换

配置多个命名模型，运行中用 `/models use <name>` 切换：

```yaml
llm:
  deepseek_anthropic:
    provider: deepseek
    api_type: anthropic
    api_key: sk-xxx
    base_url: https://api.deepseek.com/anthropic
    models:
      - name: deepseek-v4-pro
      - name: deepseek-v4-flash
  deepseek_openai:
    provider: deepseek
    api_type: openai
    api_key: sk-xxx
    base_url: https://api.deepseek.com
    models:
      - name: deepseek-v4-pro
      - name: deepseek-v4-flash
```
