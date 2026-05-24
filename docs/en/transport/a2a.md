# A2A Transport — Google Agent-to-Agent

The A2A transport implements the [Google Agent-to-Agent (A2A) protocol](https://github.com/google/A2A) using JSON-RPC 2.0 over HTTP. It allows Dolphin to be discovered and called by other A2A-compatible agents.

> **Status**: Inbound only (v1). Outbound calls planned for v2.

## Configuration

```yaml
transport:
  a2a:
    enabled: true
    listen_addr: ":8334"
    agent_id: dolphin
    agent_name: Dolphin AI Agent
    agent_version: "0.1.0"
    agent_description: "Cross-terminal/email/chat/SSH AI agent"
    capabilities:
      - task-execution
      - shell-command
      - web-search
    sync_timeout: 60s
    api_key: ""                     # empty = no auth
    tls_enabled: false
    tls_cert_file: ""
    tls_key_file: ""
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable A2A transport |
| `listen_addr` | string | `":8334"` | HTTP listen address |
| `agent_id` | string | `"dolphin"` | Unique agent identifier |
| `agent_name` | string | `"Dolphin AI Agent"` | Human-readable agent name |
| `agent_version` | string | `"0.1.0"` | Agent version |
| `agent_description` | string | — | Agent description |
| `capabilities` | []string | — | Capability list (for agent discovery) |
| `sync_timeout` | string | `"60s"` | Max wait time for synchronous task execution |
| `api_key` | string | `""` | Bearer token auth. Empty = auth disabled |
| `tls_enabled` | bool | `false` | Enable TLS |
| `tls_cert_file` | string | `""` | TLS cert file path |
| `tls_key_file` | string | `""` | TLS key file path |

## Usage

### 1. Enable in config

```yaml
transport:
  a2a:
    enabled: true
    listen_addr: ":8334"
```

### 2. Start Dolphin

```bash
dolphin
```

Look for the banner: `=== A2A transport active (Google Agent-to-Agent) on :8334 ===`

### 3. Discover the agent card

```bash
curl http://localhost:8334/.well-known/agent.json
```

### 4. Send a task

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"test-001","message":{"role":"user","parts":[{"type":"text","text":"几点了?"}]}}}'
```

### 5. Query or cancel (optional)

```bash
# Query task status
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"test-001"}}'

# Cancel a running task
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"test-001"}}'
```

## API Endpoints

### `/.well-known/agent.json` (GET)

Agent capability discovery. Returns metadata about this agent.

```bash
curl http://localhost:8334/.well-known/agent.json
```

Response:
```json
{
  "name": "Dolphin AI Agent",
  "description": "Cross-terminal/email/chat/SSH AI agent",
  "url": "http://localhost:8334/a2a",
  "version": "0.1.0",
  "protocolVersion": "1.0",
  "capabilities": {
    "streaming": false,
    "pushNotifications": false,
    "stateTransitionHistory": false
  },
  "securitySchemes": {},
  "security": [],
  "defaultInputModes": ["text/plain"],
  "defaultOutputModes": ["text/plain"]
}
```

### `/a2a` (POST)

JSON-RPC 2.0 endpoint for A2A task operations.

#### JSON-RPC Methods

| Method | Description | Async |
|--------|-------------|:-----:|
| `tasks/send` | Submit a task and wait for result | ❌ (sync) |
| `tasks/get` | Query a submitted task's status | ✅ |
| `tasks/cancel` | Cancel a running task | ✅ |

#### tasks/send

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0","id":1,"method":"tasks/send",
    "params":{
      "id":"test-001",
      "message":{"role":"user","parts":[{"type":"text","text":"几点了?"}]}
    }
  }'
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "test-001",
    "status": {
      "state": "completed",
      "message": {
        "role": "agent",
        "parts": [{"type": "text", "text": "2026-05-22 10:24"}]
      }
    }
  }
}
```

#### tasks/get

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0","id":2,"method":"tasks/get",
    "params":{"id":"test-001"}
  }'
```

#### tasks/cancel

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0","id":3,"method":"tasks/cancel",
    "params":{"id":"test-001"}
  }'
```

## Authentication

Configure `api_key` in the A2A config. Clients must then include:

```
Authorization: Bearer <api_key>
```

```bash
curl -X POST http://localhost:8334/a2a \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{...}}'
```

When `api_key` is empty, all requests are accepted without authentication.

## Task States

```
submitted → working → completed
                  → failed
                  → canceled
                  → rejected
```

## Limitations (v1)

- Synchronous only — `tasks/send` blocks the HTTP handler until the agent responds or the timeout expires
- In-memory task storage — tasks are lost on restart
- No streaming responses
- No push notifications

## See Also

- [Google A2A Specification](https://github.com/google/A2A)
- [Design Doc](../../design/modules/a2a-transport.md)

---

> Last modified: 2026-05-22
