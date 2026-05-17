# MCP Tool System (`internal/mcp/`)

## Interface

```go
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error)
}
```

## Built-in Tools

| Tool | File | Capability |
|------|------|------------|
| Shell | `shell.go` | 执行命令, 白名单, 超时, 路径安全 |
| CDP | `cdp.go` | 浏览器: Navigate/Click/Screenshot/Evaluate/GetText |
| Email | `email.go` | SMTP 发送 + IMAP/POP3 搜索/取回 |
| Webhook | `webhook.go` | HTTP 请求 (配置 target / inline URL) |

## CDP Tool Flow

```mermaid
sequenceDiagram
    participant LLM
    participant CDP as CDPTool.Execute()
    participant Browser as chromedp
    participant FS as Filesystem

    LLM->>CDP: {"action": "screenshot", "selector": "#kw"}
    CDP->>CDP: getBrowser() — 获取或创建 browserCtx
    alt WsURL configured
        CDP->>Browser: chromedp.NewRemoteAllocator(wsURL)
    else no WsURL
        CDP->>Browser: findBrowser() 查找 Chrome/Chromium/Edge
        CDP->>Browser: chromedp.NewExecAllocator(headless)
    end

    alt selector provided
        CDP->>Browser: chromedp.Screenshot(selector, &buf)
    else no selector
        CDP->>Browser: chromedp.FullScreenshot(&buf, 100)
    end
    Browser-->>CDP: buf []byte

    CDP->>FS: os.WriteFile(screenshot_<timestamp>.png, buf)
    CDP-->>LLM: "Screenshot saved: .dolphin/screenshots/... (1874 bytes)"
```

## External Server Connection Flow

```mermaid
sequenceDiagram
    participant R as Registry
    participant C as ServerClient
    participant T as Transport

    R->>C: NewServerClient(name, config)
    alt type=stdio
        C->>T: newStdioTransport(name, cfg)
    else type=sse
        C->>T: newSSETransport(name, cfg)
    else type=http-stream
        C->>T: newHTTPStreamTransport(name, cfg)
    end

    C->>T: connect(ctx) — 建立连接
    C->>C: initialize() — MCP handshake
    Note over C: jsonrpc "initialize"<br/>protocolVersion "2024-11-05"<br/>→ notifications/initialized
    C->>T: sendRequest("tools/list")
    T-->>C: tool definitions
    C-->>R: []ToolDefinition
```

## Progressive Disclosure

- 默认展示 Top 10 工具 (按 CallCount)
- 始终暴露 `search_mcp_tools`
- `Clone()` — per-coordinator 独立副本
- `FilteredView(names)` — 子 Agent 工具子集
- 自动统计: CallCount, ErrorCount, LastCalledAt, TotalDuration

<!-- last-modified: 2026-05-17 -->
