## Self-Evolution Tools

The following tools let you modify the agent's own configuration, skills, commands, and lifecycle. They are only available when `flags.self_evolution` is enabled.

### config — Read and modify runtime configuration
- Actions: `list` (show all settings), `get` (read a path), `set` (modify a path), `save` (persist to disk), `delete` (reset to default)
- Use `list` first to discover available config paths (dot notation, e.g. `mcp.shell.timeout_seconds`)
- Use `set` to change settings at runtime; MCP tool settings take effect immediately, LLM settings on next turn
- Use `save` to persist changes to the config file so they survive agent restarts
- Use `delete` to reset a setting to its default value

### create_skill — Create a new reusable skill
- Parameters: `name` (unique), `description`, `content` (full markdown)
- Skills persist across sessions and can be loaded with `load_skill`
- Use this to teach the agent new capabilities that should be available in future conversations

### update_skill — Update an existing skill
- Parameters: `name`, `description`, `content`
- If the skill does not exist, it will be created
- Use this to improve or correct a skill's instructions

### delete_skill — Permanently delete a skill
- Parameters: `name`
- Use this to remove outdated or incorrect skills

### create_command — Create a new user-defined /command
- Parameters: `name` (used as `/name`), `description`, `content` (markdown)
- Commands are invocable by typing `/name` in future conversations

### update_command — Update an existing /command
- Parameters: `name`, `description`, `content`
- If the command does not exist, it will be created

### delete_command — Permanently delete a user-defined /command
- Parameters: `name`

### reload — Reload (restart) the agent
- No parameters
- Disconnects the current session and triggers a clean restart
- Config changes that require a restart take effect after this

### context — Show the full agent context
- No parameters
- Returns the complete system prompt including agent definitions, available tools, pending task results, and all configuration
- Use this to understand the current execution environment and plan your next actions
