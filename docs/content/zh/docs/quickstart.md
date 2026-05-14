---
title: 快速开始
description: 5 分钟上手小海豚
slug: quickstart
weight: 5
---

5 分钟让小海豚跑起来。你需要一个 LLM API 密钥和一个终端。

## 1. 安装小海豚

**macOS / Linux：**

```bash
curl -sfL https://github.com/dolphinZzv/dolphin/releases/latest/download/install.sh | sh
dolphin --version
```

**Go 安装**（需 Go 1.26+）：

```bash
go install github.com/dolphinZzv/dolphin@latest
```

**Windows（PowerShell）：**

```powershell
$VERSION = "v1.0.0"
Invoke-WebRequest -Uri "https://github.com/dolphinZzv/dolphin/releases/download/$VERSION/dolphin_${VERSION}_windows_x86_64.zip" -OutFile "dolphin.zip"
Expand-Archive -Path "dolphin.zip" -DestinationPath .
Move-Item .\dolphin.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\dolphin.exe"
```

详见[完整安装指南]({{< relref "docs/install" >}})。

## 2. 设置 API 密钥

```bash
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="claude-sonnet-4-6"
```

或使用其他兼容的提供商：

```bash
export DZ_LLM_API_KEY="your-deepseek-key"
export DZ_LLM_BASE_URL="https://api.deepseek.com/v1"
export DZ_LLM_MODEL="deepseek-v4-flash"
```

## 3. 启动小海豚

```bash
dolphin
```

首次运行会启动设置向导：

1. **选择称呼** — 小海豚怎么称呼你
2. **生成配置** — 可选保存默认 `~/.dolphin/config.yaml`
3. **生成技能** — 可选保存入门技能文件

设置完成后会出现提示符：

```
Dolphin >
```

## 4. 试试看

```
Dolphin > 这个目录下有哪些文件？

Dolphin > 创建一个 hello.txt，内容写上"你好，小海豚！"

Dolphin > 今天天气怎么样？
  ─ 我来查一下你所在位置的天气。
```

小海豚可以执行命令、读写文件、浏览网页等等。

## 5. 接下来

- **[配置参考]({{< relref "docs/config" >}})** — 自定义提供商、传输层和工具
- **[安装指南]({{< relref "docs/install" >}})** — 所有安装方式和故障排查
