# dolphin

<p align="center">
  <strong>Docs:</strong> <a href="README.md">English</a> · <a href="README.zh.md">中文</a>
  <br>
  <strong>Distribution:</strong> <a href="https://github.com/dolphinZzv/dolphin">GitHub</a> · <a href="https://gitee.com/dolphinzzv/dolphindolphin">Gitee</a>
  <br>
  <strong>Install:</strong> <a href="docs/en/INSTALL.md">Install Guide</a>
</p>

An AI agent that lives where you work — terminal, email, chat, or SSH. It runs shell commands, controls a browser, delegates work to sub-agents, and follows schedules you define. Think of it as a capable teammate that connects through whatever channel suits the task.

## Why dolphin?

Most AI coding tools lock you into a specific editor or a web UI. That's fine for writing code, but real work sprawls. You might want to ask the agent something over email while you're on your phone. Or have it run a scheduled task every evening without anyone touching a keyboard. Or SSH into a server and ask the agent sitting there to diagnose an issue.

dolphin doesn't care which door you knock on — it answers them all. The same agent, the same tools, the same session state, regardless of transport.

## What it can do

**Run commands and automate workflows.** The shell tool gives it access to your filesystem, git, package managers, build tools — anything you'd type into a terminal. Timeouts and optional allowlists keep it safe.

**Drive a browser.** Through the CDP (Chrome DevTools Protocol) tool, it can open pages, click around, fill forms, take screenshots, and extract data. Useful for testing, scraping, or automating web tasks that don't have an API.

**Coordinate multiple agents.** Need a code review, a security audit, and a deployment check at the same time? The coordinator dispatches tasks to specialized sub-agents that run in parallel. You define persistent agents for recurring roles, or the coordinator creates temporary ones on the fly.

**Learn skills on demand.** Skills are markdown files that teach the agent how to do specific things — code review patterns, deployment checklists, database migration steps. The agent loads only what it needs, when it needs it, so the system prompt stays lean.

**Follow a schedule.** Drop a CRONTAB.md in your project and the agent will run tasks on a cron schedule — daily summaries, weekly maintenance, whatever rhythm your project needs. Results show up in the session like any other agent output.

**Plug into external tools.** Any MCP-compatible server (database inspectors, API explorers, code linters) can be wired in through config. The agent discovers available tools and uses them when relevant.

## How to connect

dolphin speaks four transports, and you can enable any combination of them:

- **stdio** — the default. Run `./dolphin-ai` and chat in your terminal. First run walks you through setting up your profile and recommended tools.
- **SSH** — connect from anywhere. `ssh dolphin-ai@host -p 2222`. Same agent session, terminal interface.
- **MQTT** — lightweight pub/sub messaging. Great for embedded devices, chat apps, or event-driven automation. Ships with a native macOS client (Panda).
- **Email** — send a command as an email subject, get the response back. Polls IMAP on a configurable interval.

All transports share the same agent instance, tools, and session state. Switch between them freely.

## Getting started

### Quick start

```bash
go build -o dolphin-ai .
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="gpt-4o"          # model name
export DZ_LLM_BASE_URL="https://api.openai.com/v1"  # optional, for custom endpoints
./dolphin-ai
```

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DZ_LLM_API_KEY` | **yes** | — | LLM API key |
| `DZ_LLM_MODEL` | **yes** | — | Model name (e.g. `gpt-5.5-instant`, `claude-sonnet-4-6`, `deepseek-v4-flash`) |
| `DZ_LLM_BASE_URL` | **yes** | — | API base URL (e.g. `https://api.openai.com/v1`, `https://api.deepseek.com/v1`) |
| `DZ_LLM_TYPE` | **yes** | — | Provider type: `openai` or `anthropic` |

### First-run flow

On first run, dolphin walks you through setup:

1. **Career profile** — pick your role (frontend, backend, devops, data, etc.). The agent recommends matching skills and MCP tools.
2. **SYSTEM.md** — optionally generate a system info file so the agent knows your OS, shell, and environment.
3. **Config file** — optionally generate `.dolphin/config.yaml` with all defaults pre-filled and commented.

Everything happens interactively in the terminal. No data leaves your machine.

To re-run the wizard later: `./dolphin-ai setup`

### Configuration

Config lives in `.dolphin/config.yaml` (project-level) or `~/.dolphin/config.yaml` (user-level). Project config overrides user config. All settings have sensible defaults — a working setup only needs an API key.

```bash
# Minimal: env vars only, no config file needed
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="gpt-4o"
./dolphin-ai
```

## Build from source

dolphin supports **Linux**, **macOS**, and **Windows** (arm64 and x86_64). See [INSTALL.md](docs/en/INSTALL.md) for all options (pre-built binaries, go install, etc.).

Quick reference:

```bash
git clone https://github.com/dolphinZzv/dolphin.git
cd dolphin

# Linux / macOS
make build              # development build (version = dev)
make build VERSION=v1.0.0   # release build

# Windows (PowerShell)
go build -o dolphin-ai.exe .   # development build

# Windows (with make, via Chocolatey/winget)
make build                  # development build
```

Documentation lives in `design/`.

## Philosophy

dolphin is built around a few beliefs about how AI agents should work:

- **Meet people where they are.** The agent shouldn't require a special UI. It should plug into the tools and channels already in use.
- **Progressive disclosure.** Show the most relevant tools and skills first. Let the LLM search for more when needed. Don't flood the context window.
- **Local first, privacy respecting.** Career profile, SYSTEM.md, session files — all stored locally. Nothing gets sent anywhere except the LLM API calls you configure.
- **Recoverable by design.** Sessions persist to disk. If the agent crashes or you shut down, you can resume where you left off. Logs rotate but don't disappear.
- **Testable and observable.** Structured logging, Prometheus metrics, pprof endpoints, and a test suite that enforces race detection and coverage gates.

---

**Contact:** [dolphin@siciv.space](mailto:dolphin@siciv.space)
