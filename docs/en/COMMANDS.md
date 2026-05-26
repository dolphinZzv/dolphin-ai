# Commands

Dolphin provides built-in slash commands available in any transport (terminal, SSH, email, chat, etc.). Use `/help` in-session to see the full list.

## Session Management

| Command | Description |
|---------|-------------|
| `/exit` or `exit` or `quit` | Exit the agent |
| `/new` | Start a fresh session (the previous session is summarized first) |
| `/status` | Show current session and agent status |
| `/cancel [id]` | Cancel all running tasks, or a specific task by ID |
| `/reload` | Reload (restart) the agent |

## Information

| Command | Description |
|---------|-------------|
| `/help` | Show this help text |
| `/mcp` | List all registered MCP tools with descriptions |
| `/agents [name]` | List agents and their status; specify a name for detailed info |
| `/skills [sub]` | List available skills. Subcommands: `new`, `delete`, `show` |
| `/commands [sub]` | List user-defined commands. Subcommands: `new`, `delete`, `show` |
| `/workflow [sub]` | List available workflows. Subcommands: `new`, `delete`, `show` |
| `/sessions [sub]` | List past sessions. Subcommand: `dump <id>` |
| `/context [sub]` | Show context summary. Subcommands: `system`, `current`, `<section>` |
| `/transport` | Show enabled transports |

## Configuration

| Command | Description |
|---------|-------------|
| `/config [sub]` | View or modify configuration. Subcommands: `get`, `set` |
| `/model [name]` | List or switch the LLM model |
| `/provider [sub]` | List or switch the LLM provider. Subcommand: `switch [name]` |

## Tasks

| Command | Description |
|---------|-------------|
| `/crontab` | View scheduled cron tasks |

## Other

| Command | Description |
|---------|-------------|
| `/forget <name>` | Reset conversation context for a specific agent |
| `/feedback` | Send feedback to the development team via email |

## Usage Notes

- Subcommands are specified as `/command subcommand [args]`. For example: `/skills new`
- Use `/help <command>` for detailed usage on a specific command
- Commands work in all transport modes — terminal, SSH, email, MQTT, and DingTalk
- User-defined commands (from `.dolphin/commands/`) are also invoked with `/` prefix
