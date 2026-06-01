---
title: 文档
---

## 快速开始

Dolphin 是一个多平台 AI 代理，可以通过钉钉、企业微信、邮件或命令行与您交互。

### 配置

Dolphin 使用 YAML 配置文件，默认路径为 `config.yaml`：

```yaml
llm:
  provider: openai
  model: gpt-4o
  openai:
    api_key: your-api-key

transport:
  - type: dingtalk
    ...
```

### 运行

```bash
./dolphin -c config.yaml
```
