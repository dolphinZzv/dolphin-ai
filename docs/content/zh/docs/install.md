---
title: 安装
description: 在 Linux、macOS 或 Windows 上安装小海豚
slug: install
weight: 10
---

小海豚支持 **Linux**、**macOS** 和 **Windows** 系统。选择最适合你的安装方式。

## 前置要求

- **LLM API 密钥** — 来自 DeepSeek、MiniMax、Kimi、智谱 GLM、通义千问，或 Anthropic
- **Go 1.26+**（仅源码编译时需要）

## 方式一：下载预编译二进制（推荐）

从 [latest release](https://github.com/dolphinZzv/dolphin/releases/latest) 下载对应平台的压缩包，解压后将 `dolphin` 二进制放入 `PATH`。

| 平台 | 文件名 |
|------|--------|
| Linux x86_64 | `dolphin_<版本>_linux_x86_64.tar.gz` |
| Linux arm64 | `dolphin_<版本>_linux_arm64.tar.gz` |
| macOS Intel | `dolphin_<版本>_macOS_x86_64.tar.gz` |
| macOS Apple Silicon | `dolphin_<版本>_macOS_arm64.tar.gz` |
| Windows x86_64 | `dolphin_<版本>_windows_x86_64.zip` |
| Windows arm64 | `dolphin_<版本>_windows_arm64.zip` |

```bash
# 示例：Linux x86_64
VERSION="v1.0.0"   # 替换为实际最新版本号
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin_${VERSION}_linux_x86_64.tar.gz"
tar xzf "dolphin_${VERSION}_linux_x86_64.tar.gz"
sudo mv dolphin /usr/local/bin/
rm "dolphin_${VERSION}_linux_x86_64.tar.gz"
```

```bash
# 示例：macOS Apple Silicon
VERSION="v1.0.0"
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin_${VERSION}_macOS_arm64.tar.gz"
tar xzf "dolphin_${VERSION}_macOS_arm64.tar.gz"
sudo mv dolphin /usr/local/bin/
rm "dolphin_${VERSION}_macOS_arm64.tar.gz"
```

```powershell
# 示例：Windows x86_64（PowerShell）
$VERSION = "v1.0.0"
Invoke-WebRequest -Uri "https://github.com/dolphinZzv/dolphin/releases/download/$VERSION/dolphin_${VERSION}_windows_x86_64.zip" -OutFile "dolphin_${VERSION}_windows_x86_64.zip"
Expand-Archive -Path "dolphin_${VERSION}_windows_x86_64.zip" -DestinationPath .
Move-Item .\dolphin.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\dolphin.exe"
Remove-Item "dolphin_${VERSION}_windows_x86_64.zip"
```

也可将下载目录加入 `PATH` 环境变量代替移动操作。

## 方式二：使用 `go install` 安装

需要 Go 1.26+。

```bash
go install github.com/dolphinZzv/dolphin@latest
```

`dolphin` 二进制会安装到 `$GOPATH/bin` 目录（默认是 `$HOME/go/bin`）。请确保该目录已在 `PATH` 中。

安装指定版本：

```bash
go install github.com/dolphinZzv/dolphin@v1.0.0
```

## 方式三：源码编译

需要 Go 1.26+ 和 `git`。

克隆仓库：

```bash
git clone https://github.com/dolphinZzv/dolphin.git
cd dolphin
```

然后根据你的系统选择对应方式。

### Linux

`make` 直接可用：

```bash
make build   # 生成 ./dolphin（版本号 = dev）
```

或手动编译：

```bash
go build -ldflags="-X 'dolphin/cmd.Version=$(VERSION)'" -o dolphin .
```

发布版本可指定 VERSION：

```bash
make build VERSION=v1.0.0
```

### macOS

`make` 包含在 Xcode Command Line Tools 中。如未安装：

```bash
xcode-select --install
```

然后：

```bash
make build   # 生成 ./dolphin（版本号 = dev）
```

或手动编译：

```bash
go build -ldflags="-X 'dolphin/cmd.Version=$(VERSION)'" -o dolphin .
```

### Windows

**方式 A — Go build（PowerShell / cmd）：**

```powershell
# 开发版本（版本号 = dev）
go build -o dolphin.exe .

# 发布版本
$env:VERSION = "v1.0.0"
go build -ldflags="-X 'dolphin/cmd.Version=$env:VERSION'" -o dolphin.exe .
```

**方式 B — Make（Windows 原生）：**

通过以下方式安装 `make`：

```powershell
# Chocolatey
choco install make

# winget
winget install GnuWin32.Make
```

然后编译：

```powershell
make build   # 生成 ./dolphin.exe（版本号 = dev）
make build VERSION=v1.0.0
```

**方式 C — Git Bash / WSL：**

```bash
make build   # 生成 ./dolphin.exe（版本号 = dev）
```

## 验证安装

```bash
dolphin --version
```

应看到如下输出：

```
dolphin dev
```

## 配置 API 密钥

小海豚运行至少需要一个 API 密钥。通过环境变量设置：

```bash
# DeepSeek
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="deepseek-v4-flash"
export DZ_LLM_BASE_URL="https://api.deepseek.com/v1"
export DZ_LLM_TYPE="openai"

./dolphin
```

首次运行会进入设置向导 — 选择角色、选填生成配置文件和系统信息文件。所有数据均存储在本地。

### 中国地区推荐模型

#### DeepSeek
`deepseek-v4-flash` → `https://api.deepseek.com/v1`

#### MiniMax
`MiniMax-M2.7` → `https://api.minimax.chat/v1`

#### 智谱 GLM
`glm-5` → `https://open.bigmodel.cn/api/paas/v4`

#### 通义千问
`qwen3.6-max-preview` → `https://dashscope.aliyuncs.com/compatible-mode/v1`

#### Kimi
`kimi-k2.6` → `https://api.moonshot.ai/v1`

以上均设置 `DZ_LLM_TYPE=openai` 即可使用。

## 升级

使用内置的更新命令：

```bash
dolphin update          # 升级到最新版本
dolphin update v1.0.0   # 升级到指定版本
dolphin update --list   # 列出可用版本
```

或重新通过上述方式安装。

## 常见问题

### "command not found: dolphin"

二进制文件不在 `PATH` 中。将其移动到 `PATH` 包含的目录（如 `/usr/local/bin`），或将安装目录加入 `PATH`：

```bash
export PATH=$PATH:/usr/local/bin
```

### "permission denied"

确保二进制文件有执行权限：

```bash
chmod +x /path/to/dolphin
```

### 没有 Go 环境

使用方式一（下载预编译二进制）代替源码编译。

### 校验文件完整性

每个 release 附带 `checksums.txt` 文件。验证下载的压缩包：

```bash
sha256sum dolphin_*.tar.gz
# 与 release 中的 checksums.txt 对比
```
