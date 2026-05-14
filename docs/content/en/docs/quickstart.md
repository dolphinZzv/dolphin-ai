---
title: Quick Start
description: Get Dolphin running in 5 minutes
slug: quickstart
weight: 5
---

Get Dolphin running in five minutes. You'll need an LLM API key and a terminal.

## 1. Install Dolphin

**macOS / Linux:**

```bash
curl -sfL https://github.com/dolphinZzv/dolphin/releases/latest/download/install.sh | sh
dolphin --version
```

**Go install** (requires Go 1.26+):

```bash
go install github.com/dolphinZzv/dolphin@latest
```

**Windows (PowerShell):**

```powershell
$VERSION = "v1.0.0"
Invoke-WebRequest -Uri "https://github.com/dolphinZzv/dolphin/releases/download/$VERSION/dolphin_${VERSION}_windows_x86_64.zip" -OutFile "dolphin.zip"
Expand-Archive -Path "dolphin.zip" -DestinationPath .
Move-Item .\dolphin.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\dolphin.exe"
```

See the [full install guide]({{< relref "docs/install" >}}) for more options.

## 2. Set your API key

Choose your provider and set all required variables:

```bash
# Anthropic
export DZ_LLM_TYPE="anthropic"
export DZ_LLM_API_KEY="sk-ant-..."
export DZ_LLM_BASE_URL="https://api.anthropic.com/v1"
export DZ_LLM_MODEL="claude-sonnet-4-6"
```

```bash
# OpenAI
export DZ_LLM_TYPE="openai"
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_BASE_URL="https://api.openai.com/v1"
export DZ_LLM_MODEL="gpt-4o"
```

```bash
# DeepSeek
export DZ_LLM_TYPE="openai"
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_BASE_URL="https://api.deepseek.com/v1"
export DZ_LLM_MODEL="deepseek-v4-flash"
```

## 3. Start Dolphin

```bash
dolphin
```

On first run Dolphin starts a setup wizard:

1. **Choose your role** — how Dolphin addresses you
2. **Generate config** — optionally save a default `~/.dolphin/config.yaml`
3. **Generate skills** — optionally save starter skill files

Once setup completes, you'll see the prompt:

```
Dolphin >
```

## 4. Try it out

```
Dolphin > what files are in this directory?

Dolphin > create a hello.txt with "Hello, Dolphin!" in it

Dolphin > what does the weather look like today?
  ─ I'll check your location and find the weather for you.
```

Dolphin can run commands, read and write files, browse the web, and more — all from the prompt.

## 5. What's next?

- **[Configuration Reference]({{< relref "docs/config" >}})** — customize providers, transports, and tools
- **[Install Guide]({{< relref "docs/install" >}})** — all install options and troubleshooting
