---
description: An AI agent that lives where you work — terminal, email, chat, or SSH
---

Dolphin is a cross-platform AI agent that runs shell commands, controls a browser, delegates work to sub-agents, and follows schedules you define. It connects through whatever channel suits the task — local terminal, SSH, MQTT, or email.

## Why Dolphin?

Most AI coding tools lock you into a specific editor or a web UI. That's fine for writing code, but real work sprawls. You might want to ask the agent something over email while you're on your phone. Or have it run a scheduled task every evening without anyone touching a keyboard. Or SSH into a server and ask the agent sitting there to diagnose an issue.

Dolphin doesn't care which door you knock on — it answers them all. The same agent, the same tools, the same session state, regardless of transport.

## Quick Start

```bash
# Set your API key and provider
export DZ_LLM_TYPE="openai"
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_BASE_URL="https://api.openai.com/v1"
export DZ_LLM_MODEL="gpt-4o"

# Run dolphin
./dolphin
```

On the first run, Dolphin will walk you through a setup wizard — choose your role, optionally generate a config file and a system prompt file. See the [Quick Start guide](docs/quickstart/) for other providers.

## Key Features

- **Multi-transport**: Terminal, SSH, MQTT, Email — one agent everywhere
- **Rich tools**: Shell commands, browser automation (CDP), MCP tools, webhooks
- **Multi-agent**: Parallel sub-agents for complex workflows
- **Skills**: On-demand skill loading via Markdown files
- **Scheduling**: CRONTAB.md for recurring tasks
- **Session persistence**: Auto-checkpoints, summaries, diary aggregation
- **Extensible**: Plugin system with hooks and events

## Distribution

- [GitHub](https://github.com/dolphinZzv/dolphin)
- [Gitee](https://gitee.com/dolphinzzv/dolphindolphin)

<!-- last-modified: 2026-05-14 -->
