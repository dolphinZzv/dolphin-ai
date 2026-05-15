---
title: 生产部署
weight: 50
---

# 生产部署

将 Dolphin 作为后台守护进程运行，支持 Linux、macOS 和 Windows。

---

## Linux — systemd

使用提供的 systemd 服务单元文件：

```bash
# 创建系统用户
sudo useradd --system --create-home --home-dir /var/lib/dolphin --shell /usr/sbin/nologin dolphin

# 安装二进制
sudo cp /path/to/dolphin /usr/local/bin/dolphin

# 创建目录
sudo mkdir -p /var/lib/dolphin/.dolphin/logs /etc/dolphin
sudo chown -R dolphin:dolphin /var/lib/dolphin /etc/dolphin

# 配置文件
sudo cp .dolphin/config.yaml /etc/dolphin/config.yaml

# 安装服务
sudo cp deploy/dolphin.service /etc/systemd/system/dolphin.service
sudo systemctl daemon-reload
sudo systemctl enable --now dolphin

# 查看状态
sudo systemctl status dolphin
journalctl -u dolphin -f
```

### 通过 systemd drop-in 设置 API 密钥（比写在配置文件中更安全）

```bash
sudo mkdir -p /etc/systemd/system/dolphin.service.d
cat << 'EOF' | sudo tee /etc/systemd/system/dolphin.service.d/llm.conf
[Service]
Environment=DZ_LLM_API_KEY="sk-..."
Environment=DZ_LLM_MODEL="claude-sonnet-4-6"
EOF
sudo systemctl daemon-reload && sudo systemctl restart dolphin
```

---

## macOS — launchd

创建 `~/Library/LaunchAgents/com.dolphin.agent.plist`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.dolphin.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/dolphin</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>DZ_LLM_API_KEY</key>
        <string>sk-...</string>
        <key>DZ_LLM_MODEL</key>
        <string>claude-sonnet-4-6</string>
        <key>DZ_LLM_BASE_URL</key>
        <string></string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/usr/local/var/log/dolphin.log</string>
    <key>StandardErrorPath</key>
    <string>/usr/local/var/log/dolphin.log</string>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.dolphin.agent.plist
launchctl start com.dolphin.agent

# 查看状态： launchctl list | grep dolphin
# 查看日志：  tail -f /usr/local/var/log/dolphin.log
# 停止：      launchctl unload ~/Library/LaunchAgents/com.dolphin.agent.plist
```

### Homebrew

通过 dolphin tap 安装：

```bash
brew tap dolphinZzv/dolphin
brew install dolphin
brew services start dolphin
```

或从本地 formula 文件安装：

```bash
brew install --formula deploy/dolphin.rb
```

---

## Windows — Service

### SC（系统内置）

```cmd
sc create dolphin binPath="C:\path\to\dolphin.exe" start=auto
sc start dolphin
sc query dolphin
sc stop dolphin
sc delete dolphin
```

### nssm（推荐生产环境使用）

```powershell
# 安装 nssm：choco install nssm / winget install nssm
nssm install dolphin "C:\path\to\dolphin.exe"
nssm start dolphin
sc query dolphin
nssm stop dolphin
```

### PowerShell 后台任务

```powershell
$job = Start-Job -ScriptBlock { C:\path\to\dolphin.exe }
# 停止：Stop-Job $job | Remove-Job $job
```

---

## 通用方法 — nohup

```bash
nohup dolphin > ~/.dolphin/logs/agent.log 2>&1 &
kill $(pgrep -f '^dolphin$')
```

## 通用方法 — tmux / screen

```bash
tmux new-session -d -s dolphin 'dolphin'    # tmux
screen -S dolphin -dm dolphin               # screen
```

---

## 零停机更新

```bash
make build VERSION=v1.0.1

# Linux (systemd)
sudo systemctl stop dolphin
sudo cp dolphin /usr/local/bin/dolphin
sudo systemctl start dolphin

# macOS (launchd)
launchctl stop com.dolphin.agent
cp dolphin /usr/local/bin/dolphin
launchctl start com.dolphin.agent

# Windows (nssm)
nssm stop dolphin
copy dolphin.exe C:\dolphin\
nssm start dolphin
```

---

## 日志

| 方式 | 日志位置 |
|-------|---------|
| systemd | `journalctl -u dolphin -f` |
| launchd | `/usr/local/var/log/dolphin.log` |
| brew services | `$(brew --prefix)/var/log/dolphin.log` |
| nssm | `nssm dump dolphin` 查看日志配置 |
| nohup | 重定向的 stdout 位置 |
| Dolphin 会话日志 | `~/.dolphin/logs/agent.log` |
