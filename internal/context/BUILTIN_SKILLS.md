## MCP Tools Usage

### shell — Execute shell commands
- Parameters: `command` (string, required), `timeout` (int, optional seconds)
- Use for: file operations, running scripts, system interaction
- Output: stdout + stderr combined
- In restrictive mode, only allowlisted commands are allowed (ls, cat, grep, find, etc.)

### cdp — Browser automation via Chrome DevTools Protocol
- Parameters: `action` (required), plus action-specific params
- Actions:
  - `navigate` — goto a URL (+ `url`), waits for page load
  - `click` — click element by CSS selector (+ `selector`)
  - `screenshot` — capture page/element as base64 PNG (+ optional `selector`)
  - `evaluate` — run JavaScript (+ `script`, supports async/await)
  - `get_text` — extract visible text from element (+ `selector`)
- Browser state persists across calls within the same session

### email — Send and receive emails
- Parameters: `action` (required), plus action-specific params
- Actions:
  - `send` — send an email (+ `to`, `subject`, `body`)
  - `search` — search mailbox (+ `query`, `max_results`, `unread_only`)
  - `fetch` — read a specific email by sequence number (+ `seq`)
- Requires SMTP/IMAP/POP3 to be configured

### webhook — Send HTTP requests
- Parameters: use `target` (named from config) or inline `url` + optional `method`, `headers`, `body`
- Supports GET, POST, PUT, PATCH, DELETE
- Default method is POST when body is set, GET otherwise
- Prefer named targets from config for security

### search_mcp_tools — Discover available MCP tools
- Use this first to find what tools are available
- Supports filtering by keyword
- Tools must be loaded via load_mcp_tools before use

### load_mcp_tools — Activate MCP tools for the next turn
- Load tools by name so they become available as API-level tools
- Loaded tools appear in the tool list starting from the next turn
- Use search_mcp_tools first to discover tool names
