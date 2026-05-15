---
title: Production Deployment
weight: 50
---

# Production Deployment

Run Dolphin as a background daemon on Linux, macOS, or Windows.

---

## Linux — systemd

Install the provided service unit:

```bash
# Create system user
sudo useradd --system --create-home --home-dir /var/lib/dolphin --shell /usr/sbin/nologin dolphin

# Install binary
sudo cp /path/to/dolphin /usr/local/bin/dolphin

# Directories
sudo mkdir -p /var/lib/dolphin/.dolphin/logs /etc/dolphin
sudo chown -R dolphin:dolphin /var/lib/dolphin /etc/dolphin

# Config
sudo cp .dolphin/config.yaml /etc/dolphin/config.yaml

# Service
sudo cp deploy/dolphin.service /etc/systemd/system/dolphin.service
sudo systemctl daemon-reload
sudo systemctl enable --now dolphin

# Check
sudo systemctl status dolphin
journalctl -u dolphin -f
```

### systemd drop-in for API keys (more secure than embedding in config)

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

Create `~/Library/LaunchAgents/com.dolphin.agent.plist`:

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

# Check:  launchctl list | grep dolphin
# Logs:   tail -f /usr/local/var/log/dolphin.log
# Stop:   launchctl unload ~/Library/LaunchAgents/com.dolphin.agent.plist
```

### Homebrew

Via the dolphin tap (once the tap is published):

```bash
brew tap dolphinZzv/dolphin
brew install dolphin
brew services start dolphin
```

Or install directly from the provided formula:

```bash
brew install --formula deploy/dolphin.rb
```

---

## Windows — Service

### SC (built-in)

```cmd
sc create dolphin binPath="C:\path\to\dolphin.exe" start=auto
sc start dolphin
sc query dolphin
sc stop dolphin
sc delete dolphin
```

### nssm (recommended for production)

```powershell
# Install nssm: choco install nssm / winget install nssm
nssm install dolphin "C:\path\to\dolphin.exe"
nssm start dolphin
sc query dolphin
nssm stop dolphin
```

### PowerShell background job

```powershell
$job = Start-Job -ScriptBlock { C:\path\to\dolphin.exe }
# Stop: Stop-Job $job | Remove-Job $job
```

---

## All platforms — nohup (quick)

```bash
nohup dolphin > ~/.dolphin/logs/agent.log 2>&1 &
kill $(pgrep -f '^dolphin$')
```

## All platforms — tmux / screen

```bash
tmux new-session -d -s dolphin 'dolphin'    # tmux
screen -S dolphin -dm dolphin               # screen
```

---

## Zero-downtime update

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

## Logs

| Setup | Log location |
|-------|-------------|
| systemd | `journalctl -u dolphin -f` |
| launchd | `/usr/local/var/log/dolphin.log` |
| brew services | `$(brew --prefix)/var/log/dolphin.log` |
| nssm | `nssm dump dolphin` 查看日志配置 |
| nohup | wherever stdout was redirected |
| Dolphin session logs | `~/.dolphin/logs/agent.log` |
