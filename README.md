# DolphinzZ

[![CI](https://github.com/dolphinZzv/dolphin/actions/workflows/ci.yml/badge.svg)](https://github.com/dolphinZzv/dolphin/actions/workflows/ci.yml)

AI coding agent with MCP tool support, multi-agent coordination, and skills system.
Runs via stdio, SSH, or MQTT.

## Quick Start

```bash
# Build
make build

# Set your API key and run
export DZ_LLM_API_KEY="sk-..."
./dolphinzZ
```

## Configuration

Priority (higher overrides lower):
1. Environment variables (`DZ_*`)
2. Project: `.dolphinzZ/config.yaml`
3. User: `~/.dolphinzZ/config.yaml`
4. System: `/etc/dolphinzZ/config.yaml`

Key environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DZ_LLM_API_KEY` | — | API key |
| `DZ_LLM_TYPE` | `openai` | `openai` or `anthropic` |
| `DZ_LLM_MODEL` | `gpt-4o` | Model name |
| `DZ_LLM_BASE_URL` | `https://api.openai.com/v1` | API base URL |
| `DZ_LLM_MAX_TOKENS` | `4096` | Max output tokens |
| `DZ_LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |

### Example config (`.dolphinzZ/config.yaml`)

```yaml
llm:
  type: "anthropic"
  base_url: "https://api.anthropic.com"
  api_key: ""
  model: "claude-sonnet-4-20250514"
  max_tokens: 4096

session:
  dir: "./sessions"
  max_loop: 50
  max_age: "24h"

transport:
  stdio:
    enabled: true
  ssh:
    enabled: false
  mqtt:
    enabled: false

mcp:
  shell:
    enabled: true
    allowed_commands: []
    timeout_seconds: 30
  cdp:
    enabled: true
    headless: true

agent_pool:
  max_concurrency: 5
  default_timeout: 300
  workspace_dir: ".dolphinzZ/workspaces"
  idle_timeout: 600
```

## Usage

### stdio (default)

```bash
./dolphinzZ
```

Built-in commands:

- `/exit`, `/quit` — end session
- `/help` — show help and top MCP tools
- `/agents` — list available agents
- `/skills` — list available skills
- `/cancel` — cancel running tasks
- `/cancel <id>` — cancel specific task

### SSH

Enable in config, then connect:

```bash
ssh dolphinzZ@<host> -p 2222
```

### MQTT

Enable in config. Subscribe to `dolphinzZ/agent/response` and publish to `dolphinzZ/agent/command`:

```bash
mosquitto_sub -t "dolphinzZ/agent/response" &
mosquitto_pub -t "dolphinzZ/agent/command" -m "your prompt"
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `shell` | Execute shell commands with timeout control |
| `cdp` | Browser automation via CDP (navigate, click, screenshot, evaluate JS) |
| `search_mcp_tools` | Search available MCP tools by name/description |
| External | Any stdio-based MCP server (configured via `mcp.servers`) |

**Progressive disclosure**: Only the top 10 most-used tools are shown to the LLM by default. Use `search_mcp_tools` to discover more.

## Multi-Agent Coordination (v0.2)

DolphinzZ supports a coordinator-subagent architecture:

- **Coordinator**: default agent that dispatches tasks to specialized sub-agents
- **User-created agents**: persistent agents defined in `.dolphinzZ/agents/<name>/agent.yaml`
- **Coordinator-created agents**: ephemeral agents created at runtime via `create_agent`

### Coordinator tools

| Tool | Description |
|------|-------------|
| `dispatch_task` | Send a task to a sub-agent for async processing |
| `create_agent` | Create a temporary agent with custom role and tools |
| `get_agent_status` | Check agent status |
| `cancel_task` | Cancel a running task |
| `delete_agent` | Delete a temporary agent |

## Skills System

Skills are specialized capabilities defined as markdown files in `.dolphinzZ/skills/`.
Each skill has a name, description, and full instructions that can be loaded on demand.

**Progressive disclosure**: Top 10 skills by usage shown in context. Use `search_skills` and `load_skill` to find and activate more.

### Coordinator tools for skills

| Tool | Description |
|------|-------------|
| `search_skills` | Search skills by name or description |
| `load_skill` | Load full skill content into context |

### Creating a skill

```markdown
---
name: code-review
description: Perform thorough code review
---

# Code Review Skill

Detailed instructions for code review...
```

## Agent Definitions (user-created)

Create agents in `.dolphinzZ/agents/<name>/agent.yaml`:

```yaml
name: reviewer
role: You are a code review specialist...
tools: ["shell"]
workspace: ".dolphinzZ/workspaces/reviewer"
timeout: 300
```

## Development

```bash
make test    # run all tests
make build   # build binary
make fmt     # format code
make clean   # clean build artifacts
```

## Safety

- Shell commands are unrestricted by default (`allowed_commands: []`). Set explicit allowlist for production use.
- SSH password is stored in plaintext at `~/.dolphinzZ/ssh_password`. Use SSH key authentication for better security.
- Session files are retained for 24 hours by default (`session.max_age`). Old files are cleaned up automatically.
- Sub-agent workspaces are isolated, preventing cross-agent file interference.
