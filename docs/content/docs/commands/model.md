---
title: 模型管理
weight: 1
---

## `/models`

管理和切换 LLM 模型。

| 子命令 | 说明 |
|--------|------|
| `/models list` | 列出所有可用模型，显示名称、厂商、API 类型及模型标识 |
| `/models use <name>` | 切换到指定模型 |

### 示例

```text
/models list
>> deepseek/deepseek-v4-flash  deepseek   openai    deepseek-v4-flash (active)
   openai/gpt-4o                            openai    gpt-4o
  (total: 2 models)

/models use openai/gpt-4o
switched to openai/gpt-4o
```
