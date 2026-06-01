---
title: 配置
---

## LLM 配置

```yaml
llm:
  provider: openai
  model: gpt-4o
  max_retries: 2
  timeout: 60s
  openai:
    api_key: sk-xxx
    base_url: https://api.openai.com
  anthropic:
    api_key: sk-ant-xxx
```

### 多提供商

Dolphin 支持通过 `llm.{name}` 配置多个 LLM 提供商，并通过 `/models use {name}` 动态切换。

```yaml
llm:
  provider: openai
  model: gpt-4o
  openai:
    api_key: sk-xxx
  deepseek:
    api_type: openai
    provider: deepseek
    model: deepseek-v4-flash
    api_key: sk-xxx
    base_url: https://api.deepseek.com
```

## 传输层配置

```yaml
transport:
  - type: dingtalk
    ...
  - type: wework
    ...
```

## 工具配置

```yaml
tool:
  timeout: 30s
```
