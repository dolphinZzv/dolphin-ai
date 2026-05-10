# DolphinzZ

An AI agent that lives where you work — terminal, email, chat, or SSH. It runs shell commands, controls a browser, delegates work to sub-agents, and follows schedules you define. Think of it as a capable teammate that connects through whatever channel suits the task.

## Why DolphinzZ?

Most AI coding tools lock you into a specific editor or a web UI. That's fine for writing code, but real work sprawls. You might want to ask the agent something over email while you're on your phone. Or have it run a scheduled task every evening without anyone touching a keyboard. Or SSH into a server and ask the agent sitting there to diagnose an issue.

DolphinzZ doesn't care which door you knock on — it answers them all. The same agent, the same tools, the same session state, regardless of transport.

## What it can do

**Run commands and automate workflows.** The shell tool gives it access to your filesystem, git, package managers, build tools — anything you'd type into a terminal. Timeouts and optional allowlists keep it safe.

**Drive a browser.** Through the CDP (Chrome DevTools Protocol) tool, it can open pages, click around, fill forms, take screenshots, and extract data. Useful for testing, scraping, or automating web tasks that don't have an API.

**Coordinate multiple agents.** Need a code review, a security audit, and a deployment check at the same time? The coordinator dispatches tasks to specialized sub-agents that run in parallel. You define persistent agents for recurring roles, or the coordinator creates temporary ones on the fly.

**Learn skills on demand.** Skills are markdown files that teach the agent how to do specific things — code review patterns, deployment checklists, database migration steps. The agent loads only what it needs, when it needs it, so the system prompt stays lean.

**Follow a schedule.** Drop a CRONTAB.md in your project and the agent will run tasks on a cron schedule — daily summaries, weekly maintenance, whatever rhythm your project needs. Results show up in the session like any other agent output.

**Plug into external tools.** Any MCP-compatible server (database inspectors, API explorers, code linters) can be wired in through config. The agent discovers available tools and uses them when relevant.

## How to connect

DolphinzZ speaks four transports, and you can enable any combination of them:

- **stdio** — the default. Run `./dolphinzZ` and chat in your terminal. First run walks you through setting up your profile and recommended tools.
- **SSH** — connect from anywhere. `ssh dolphinzZ@host -p 2222`. Same agent session, terminal interface.
- **MQTT** — lightweight pub/sub messaging. Great for embedded devices, chat apps, or event-driven automation. Ships with a native macOS client (Panda).
- **Email** — send a command as an email subject, get the response back. Polls IMAP on a configurable interval.

All transports share the same agent instance, tools, and session state. Switch between them freely.

## Getting started

Clone the repo, build, and run:

```bash
go build -o dolphinzZ ./main.go
export DZ_LLM_API_KEY="sk-..."
./dolphinzZ
```

You'll be asked what kind of work you do — pick one or more roles and DolphinzZ recommends tools and skills that match. Everything stays local. After that, you're in a session with the coordinator.

Configuration lives in `.dolphinzZ/config.yaml` (project-level) or `~/.dolphinzZ/config.yaml` (user-level). Most things have sensible defaults, so a minimal config is just the API key via environment variable.

## Project structure

```
.dolphinzZ/
├── config.yaml          # project configuration
├── agents/              # user-defined sub-agents
│   └── reviewer/
│       └── agent.yaml
├── skills/              # on-demand skill definitions
│   └── code-review.md
├── commands/            # custom slash commands
│   └── deploy.md
├── CRONTAB.md           # scheduled tasks
└── logs/                # agent logs (rotated)
```

Documentation lives in `design/` — read the design doc and the full README there for details on configuration, MCP tools, and the multi-agent system.

## Philosophy

DolphinzZ is built around a few beliefs about how AI agents should work:

- **Meet people where they are.** The agent shouldn't require a special UI. It should plug into the tools and channels already in use.
- **Progressive disclosure.** Show the most relevant tools and skills first. Let the LLM search for more when needed. Don't flood the context window.
- **Local first, privacy respecting.** Career profile, SYSTEM.md, session files — all stored locally. Nothing gets sent anywhere except the LLM API calls you configure.
- **Recoverable by design.** Sessions persist to disk. If the agent crashes or you shut down, you can resume where you left off. Logs rotate but don't disappear.
- **Testable and observable.** Structured logging, Prometheus metrics, pprof endpoints, and a test suite that enforces race detection and coverage gates.
