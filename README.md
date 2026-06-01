# Dolphin-AI

你卓尔不凡

## 快速开始

```shell
go install github.com/dolphinZzv/dolphin-ai/cmd/dolphin@latest
```

创建 `config.yaml`：

```yaml
llm:
  deepseek_anthropic:
    provider: deepseek
    api_type: anthropic
    api_key: "sk-xxx"
    base_url: "https://api.deepseek.com/anthropic"
    models:
      - name: deepseek-v4-pro
      - name: deepseek-v4-flash
```

启动：

```shell
./dolphin
```

看到以下输出即表示运行成功：

```
hello dolphin
```
