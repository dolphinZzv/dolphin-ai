# STDIO Transport

The stdio transport reads from stdin and writes to stdout. It is the default transport used when running Dolphin interactively in a terminal.

## Configuration

```yaml
transport:
  stdio:
    enabled: true
    markdown_render: true
    markdown_style: dracula
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable stdio transport |
| `markdown_render` | bool | `true` | Render markdown output with rich formatting |
| `markdown_style` | string | `"dracula"` | Glamour markdown style (https://github.com/charmbracelet/glamour) |

## Usage

Simply run:

```bash
dolphin
```

The stdio transport starts automatically by default. You'll enter an interactive REPL where you can type prompts and commands.

## Slash Commands

Available in stdio REPL:

| Command | Description |
|---------|-------------|
| `/new` | Start a fresh session |
| `/reset` | Reset to clean state |
| `/context` | Show context summary |
| `/config` | View or modify config |
| `/help` | Show help |
| `/exit` | Exit |
| `Ctrl+C` | Interrupt / exit |

---

> Last modified: 2026-05-22
