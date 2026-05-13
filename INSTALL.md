# Install dolphin

[English](INSTALL.md) | [中文](INSTALL.zh.md)

dolphin runs on **Linux**, **macOS**, and **Windows**. Choose the method that works best for you.

## Prerequisites

- **LLM API key** — from OpenAI (or any OpenAI-compatible provider), Anthropic, or a regional LLM service
- **Go 1.26+** (only required for building from source)

## Option 1: Download a pre-built binary (recommended)

Download the archive for your platform from the [latest release](https://github.com/dolphinZzv/dolphin/releases/latest), extract the binary, and place it in your `PATH`.

| Platform | Archive name |
|----------|-------------|
| Linux x86_64 | `dolphin_<version>_linux_x86_64.tar.gz` |
| Linux arm64 | `dolphin_<version>_linux_arm64.tar.gz` |
| macOS Intel | `dolphin_<version>_macOS_x86_64.tar.gz` |
| macOS Apple Silicon | `dolphin_<version>_macOS_arm64.tar.gz` |
| Windows x86_64 | `dolphin_<version>_windows_x86_64.zip` |
| Windows arm64 | `dolphin_<version>_windows_arm64.zip` |

```bash
# Example: install the latest version on Linux x86_64
VERSION="v1.0.0"   # replace with actual latest version
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin_${VERSION}_linux_x86_64.tar.gz"
tar xzf "dolphin_${VERSION}_linux_x86_64.tar.gz"
sudo mv dolphin /usr/local/bin/
rm "dolphin_${VERSION}_linux_x86_64.tar.gz"
```

```bash
# macOS Apple Silicon example
VERSION="v1.0.0"
curl -LO "https://github.com/dolphinZzv/dolphin/releases/download/${VERSION}/dolphin_${VERSION}_macOS_arm64.tar.gz"
tar xzf "dolphin_${VERSION}_macOS_arm64.tar.gz"
sudo mv dolphin /usr/local/bin/
rm "dolphin_${VERSION}_macOS_arm64.tar.gz"
```

```powershell
# Windows x86_64 example (PowerShell)
$VERSION = "v1.0.0"
Invoke-WebRequest -Uri "https://github.com/dolphinZzv/dolphin/releases/download/$VERSION/dolphin_${VERSION}_windows_x86_64.zip" -OutFile "dolphin_${VERSION}_windows_x86_64.zip"
Expand-Archive -Path "dolphin_${VERSION}_windows_x86_64.zip" -DestinationPath .
Move-Item .\dolphin.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\dolphin.exe"
Remove-Item "dolphin_${VERSION}_windows_x86_64.zip"
```

Alternatively, add the download directory to your `PATH` instead of moving the binary.

## Option 2: Install with `go install`

Requires Go 1.26+.

```bash
go install github.com/dolphinZzv/dolphin@latest
```

This places the `dolphin` binary in `$GOPATH/bin` (or `$HOME/go/bin` by default). Make sure that directory is in your `PATH`.

To install a specific version:

```bash
go install github.com/dolphinZzv/dolphin@v1.0.0
```

## Option 3: Build from source

```bash
git clone https://github.com/dolphinZzv/dolphin.git
cd dolphin
make build   # produces ./dolphin
# or manually:
# go build -ldflags="-X 'dolphin/cmd.Version=$(version)'" -o dolphin .
```

For development builds (version set to `dev`):

```bash
make build
# or
go build -o dolphin .
```

## Verify the installation

```bash
dolphin --version
```

You should see output like:

```
dolphin dev
```

## Post-installation: configure your API key

dolphin needs at least an API key to run. Set it via environment variable:

```bash
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="gpt-4o"
./dolphin
```

On the first run, dolphin will walk you through a setup wizard — choose your role, optionally generate a config file and a system prompt file. Everything is stored locally.

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
