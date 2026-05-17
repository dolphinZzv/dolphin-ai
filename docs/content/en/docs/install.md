---
title: Install
description: Install Dolphin on Linux, macOS, or Windows
slug: install
weight: 10
---

Dolphin runs on **Linux**, **macOS**, and **Windows**. Choose the method that works best for you.

## Prerequisites

- **LLM API key** — from OpenAI (or any OpenAI-compatible provider), Anthropic, or a regional LLM service
- **Go 1.26+** (only required for building from source)

## Option 1: Download a pre-built binary (recommended)

Download the archive for your platform from the [latest release](https://github.com/dolphinZzv/dolphin/releases/latest), extract the binary, and place `dolphin-ai` in your `PATH`.

| Platform | Archive name |
|----------|-------------|
| Linux x86_64 | `dolphin-ai_<version>_linux_x86_64.tar.gz` |
| Linux arm64 | `dolphin-ai_<version>_linux_arm64.tar.gz` |
| macOS Intel | `dolphin-ai_<version>_macOS_x86_64.tar.gz` |
| macOS Apple Silicon | `dolphin-ai_<version>_macOS_arm64.tar.gz` |
| Windows x86_64 | `dolphin-ai_<version>_windows_x86_64.zip` |
| Windows arm64 | `dolphin-ai_<version>_windows_arm64.zip` |

```bash
# Example: install the latest version on Linux x86_64
VERSION="v1.0.0"   # replace with actual latest version
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin-ai_${VERSION}_linux_x86_64.tar.gz"
tar xzf "dolphin-ai_${VERSION}_linux_x86_64.tar.gz"
sudo mv dolphin-ai /usr/local/bin/
rm "dolphin-ai_${VERSION}_linux_x86_64.tar.gz"
```

```bash
# macOS Apple Silicon example
VERSION="v1.0.0"
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin-ai_${VERSION}_macOS_arm64.tar.gz"
tar xzf "dolphin-ai_${VERSION}_macOS_arm64.tar.gz"
sudo mv dolphin-ai /usr/local/bin/
rm "dolphin-ai_${VERSION}_macOS_arm64.tar.gz"
```

```powershell
# Windows x86_64 example (PowerShell)
$VERSION = "v1.0.0"
Invoke-WebRequest -Uri "https://github.com/dolphinZzv/dolphin/releases/download/$VERSION/dolphin-ai_${VERSION}_windows_x86_64.zip" -OutFile "dolphin-ai_${VERSION}_windows_x86_64.zip"
Expand-Archive -Path "dolphin-ai_${VERSION}_windows_x86_64.zip" -DestinationPath .
Move-Item .\dolphin-ai.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\dolphin-ai.exe"
Remove-Item "dolphin-ai_${VERSION}_windows_x86_64.zip"
```

Alternatively, add the download directory to your `PATH` instead of moving the binary.

## Option 2: Install with `go install`

Requires Go 1.26+.

```bash
go install github.com/dolphinZzv/dolphin@latest
```

This places the `dolphin-ai` binary in `$GOPATH/bin` (or `$HOME/go/bin` by default). Make sure that directory is in your `PATH`.

To install a specific version:

```bash
go install github.com/dolphinZzv/dolphin@v1.0.0
```

## Option 3: Build from source

Requires Go 1.26+ and `git`.

Clone the repo:

```bash
git clone https://github.com/dolphinZzv/dolphin.git
cd dolphin-ai
```

Then follow the instructions for your platform.

### Linux

`make` is available out of the box:

```bash
make build   # produces ./dolphin-ai (version = dev)
```

Or build manually:

```bash
go build -ldflags="-X 'dolphin/cmd.Version=$(VERSION)'" -o dolphin-ai .
```

For a release build, set VERSION:

```bash
make build VERSION=v1.0.0
```

### macOS

`make` is included with Xcode Command Line Tools. Install them if you haven't already:

```bash
xcode-select --install
```

Then:

```bash
make build   # produces ./dolphin-ai (version = dev)
```

Or build manually:

```bash
go build -ldflags="-X 'dolphin/cmd.Version=$(VERSION)'" -o dolphin-ai .
```

### Windows

**Option A — Go build (PowerShell / cmd):**

```powershell
# Development build (version = dev)
go build -o dolphin-ai.exe .

# Release build with version
$env:VERSION = "v1.0.0"
go build -ldflags="-X 'dolphin/cmd.Version=$env:VERSION'" -o dolphin-ai.exe .
```

**Option B — Make (native Windows):**

Install `make` via one of:

```powershell
# Chocolatey
choco install make

# winget
winget install GnuWin32.Make
```

Then build:

```powershell
make build   # produces ./dolphin-ai.exe (version = dev)
make build VERSION=v1.0.0
```

**Option C — Git Bash / WSL:**

```bash
make build   # produces ./dolphin-ai.exe (version = dev)
```

## Verify the installation

```bash
dolphin-ai --version
```

You should see output like:

```
dolphin-ai dev
```

## Post-installation: configure your API key

Dolphin needs at least an API key to run. Set it via environment variable:

```bash
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="claude-sonnet-4-6"
./dolphin-ai
```

On the first run, Dolphin will walk you through a setup wizard — choose your role, optionally generate a config file and a system prompt file. Everything is stored locally.

### Recommended models

#### OpenAI
`gpt-4o` → `https://api.openai.com/v1`

#### Anthropic
`claude-sonnet-4-6` → `https://api.anthropic.com/v1`

#### DeepSeek
`deepseek-v4-flash` → `https://api.deepseek.com/v1`

#### MiniMax
`MiniMax-M2.7` → `https://api.minimax.chat/v1`

#### Zhipu GLM
`glm-5` → `https://open.bigmodel.cn/api/paas/v4`

#### Qwen
`qwen3.6-max-preview` → `https://dashscope.aliyuncs.com/compatible-mode/v1`

#### Kimi
`kimi-k2.6` → `https://api.moonshot.ai/v1`

Set `DZ_LLM_TYPE=openai` for OpenAI-compatible APIs, or `DZ_LLM_TYPE=anthropic` for Anthropic.

## Updating

Use the built-in update command:

```bash
dolphin update          # update to the latest release
dolphin update v1.0.0   # update to a specific version
dolphin update --list   # list available versions
```

Or re-install using one of the methods above.

## Troubleshooting

### "command not found: dolphin"

The binary isn't in your `PATH`. Either move it to a directory in your `PATH` (e.g. `/usr/local/bin`) or add the install directory:

```bash
export PATH=$PATH:/usr/local/bin
```

### "permission denied"

Make sure the binary is executable:

```bash
chmod +x /path/to/dolphin
```

### "Go not found"

Download a pre-built binary (Option 1) instead of building from source.

### Checksum verification

Each release includes a `checksums.txt` file. Verify your download:

```bash
sha256sum dolphin_*.tar.gz
# compare against checksums.txt from the release
```

<!-- last-modified: 2026-05-17 -->
