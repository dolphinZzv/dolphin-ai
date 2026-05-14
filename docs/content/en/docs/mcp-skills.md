---
title: MCP & Skills
description: How to extend Dolphin with external MCP servers and loadable skills
slug: mcp-skills
weight: 30
---

Dolphin can be extended with external tools and specialized capabilities through two complementary systems: **MCP servers** (Model Context Protocol) and **Skills** (domain-specific knowledge packs).

---

## MCP Servers

MCP servers expose tools the agent can call — shell commands, browser automation, email, issue trackers, or any custom service. Tools from MCP servers appear in the agent's tool list and can be called by the LLM during normal conversation.

### Built-in MCP Tools

Dolphin ships with several built-in MCP tools:

| Tool | Config key | Description |
|------|-----------|-------------|
| Shell | `mcp.shell` | Execute shell commands locally |
| CDP Browser | `mcp.cdp` | Browser automation via Chrome DevTools Protocol |
| Email | `mcp.email` | Send, search, and fetch emails |
| Webhook | `mcp.webhook` | Send HTTP requests to external services |

These can be toggled on/off and configured independently. Details are in the [Configuration Reference]({{< relref "docs/config" >}}).

### External MCP Servers

Connect to third-party MCP servers over three transport types:

**stdio** — spawn the server as a subprocess:

```yaml
mcp:
  servers:
    filesystem:
      type: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
```

**sse** — connect to a remote SSE stream (used by [chick](https://github.com/dolphinv/chick) and other servers):

```yaml
mcp:
  servers:
    chick:
      type: sse
      url: "https://chick.example.com/mcp"
      headers:
        Authorization: "Bearer your-token"
      timeout: 30
```

**http-stream** — similar to SSE but uses chunked transfer encoding:

```yaml
mcp:
  servers:
    my-server:
      type: http-stream
      url: "https://mcp.example.com/stream"
```

### Loading Process

When Dolphin starts:

1. Each configured server is connected and initialized via the MCP handshake (`initialize` → `notifications/initialized`).
2. Tools are discovered via `tools/list` and registered with the server name as prefix (e.g. `chick:search_issues`).
3. If a server fails to start, it is skipped with a warning — other servers still load.
4. Tool definitions (name, description, input schema) are injected into the LLM context each turn.

### Troubleshooting

- **Server fails to connect**: check the URL, network access, and auth headers.
- **Tools show empty schema**: the server may need a newer MCP protocol version. Dolphin sends `2024-11-05`.
- **Call fails with "jsonrpc error"**: usually a parameter validation error from the server — check the tool's schema.
- **View loaded tools**: start Dolphin and look for the `Loaded MCP tools:` line at startup, or type `/status`.

---

## Skills

Skills are markdown files containing domain expertise that the agent loads on demand. Think of them as on-demand system prompts — when you ask the agent to `load_skill react-best-practices`, React-specific patterns and knowledge are injected into the next turn's context.

### Skill File Format

Each skill is a `.md` file with optional YAML frontmatter:

```markdown
---
name: react-best-practices
description: React best practices — hooks, state management, performance
---

# React Best Practices

## Hooks

- Use `useCallback` for event handlers passed as props
- Use functional state updates to avoid stale closures: `setCount(c => c + 1)`

## State Management

- Prefer `useReducer` over `useState` when state logic is complex
- ...
```

If frontmatter is omitted, the filename (without `.md`) is used as the skill name and the first heading as the description.

### Configuration

```yaml
skills:
  dir: .dolphin/skills        # where skill .md files live
  max_top: 10                 # how many top skills to show in the system prompt
  repos:                      # community skill manifests
    - dolphinv/skills
```

- **`dir`**: Directory scanned for `.md` files at startup. Hot-reloaded when files change.
- **`max_top`**: The top N skills (by usage count) are listed in the system prompt for discovery.
- **`repos`**: GitHub repos containing a `skills.json` manifest. Skills are downloaded and merged into `dir`.

### Using Skills

During a session, use these commands:

| Command | Action |
|---------|--------|
| `/skills` | List all available skills |
| `load_skill <name>` | Load a skill into the current context |

Or let the agent search and load naturally — the LLM can call `search_skills` and `load_skill` as tools when it thinks domain knowledge would help.

The top skills (by call count, up to `max_top`) are shown in every system prompt for quick discovery:

```
Available Skills
Skills are specialized capabilities you can load on demand with load_skill.

  react-best-practices — React best practices — hooks, state management, performance
  backend-golang — Go backend development — Gin/GRPC/microservices
  ...
```

### Writing Custom Skills

1. Create a `.md` file in the skills directory:

   ```bash
   mkdir -p .dolphin/skills
   ```

2. Write the skill with frontmatter and body content. The body is injected into the LLM context when loaded, so keep it focused:

   ```markdown
   ---
   name: my-api-conventions
   description: Our team's REST API conventions — error codes, auth, pagination
   ---

   # API Conventions

   - Error format: `{"error": {"code": "...", "message": "..."}}`
   - Auth: Bearer token in `Authorization` header
   - Pagination: `?page=N&limit=M`, response includes `total` field
   ```

3. Skills are auto-detected on next start or after a file change (hot-reload).

### Community Skills

Set `skills.repos` to pull from a community manifest. The manifest is a `skills.json` file listing available skills:

```json
{
  "version": "1.0",
  "description": "dolphin Official Skills Repository",
  "repo_url": "https://github.com/dolphinv/skills",
  "tools": [
    {
      "name": "frontend-expert",
      "description": "Frontend expert — React/Vue/Angular frameworks",
      "url": "https://github.com/dolphinv/skills/blob/main/frontend-expert/"
    }
  ]
}
```

Each entry in `tools` points to a skill that can be downloaded and loaded.

### Hot Reload

Skills are watched for file changes. When you edit a `.md` file in the skills directory, the manager reloads automatically within 5 seconds. No restart needed.

---

## Combining MCP + Skills

MCP tools and skills work together:

- **MCP tools** give the agent new *actions* (call APIs, run commands, browse web pages).
- **Skills** give the agent new *knowledge* (best practices, conventions, domain context).

Example workflow:

1. Configure a chick MCP server to track issues.
2. Create a skill with your team's issue triage conventions.
3. The agent loads the skill, learns your triage rules, then calls `chick:search_issues` and `chick:transition_issue` using those rules.
