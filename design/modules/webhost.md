# WebHost Native UI Integration

## 状态

- **版本**: v1.0 (设计完成)
- **日期**: 2026-05-17
- **评分**: 96/100

## 核心决策

| 决策 | 选择 |
|------|------|
| 传输协议 | HTTP-stream + JSON-RPC 2.0 |
| macOS | SwiftUI + WebKit |
| Windows | WPF + WebView2 |
| 截图方案 | drawHierarchy + bitmapImageRep (macOS) / CapturePreviewAsync (Win) |
| 交互模式 | OS 级透明蒙层 + JS 拦截 |
| 重连机制 | `since=timestamp` 时间窗口 |
| 心跳检测 | 每 30s 发 web/ping，60s 无消息则重连 |

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      Dolphin Agent                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   Shell     │  │   CDP       │  │   WebHost Tool      │ │
│  │   Tool      │  │   Tool      │  │   (新增)            │ │
│  └─────────────┘  └─────────────┘  └──────────┬──────────┘ │
└────────────────────────────────────────────────┼────────────┘
                                                 │ HTTP-stream
                                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                    MCP Client (Go)                             │
│  internal/mcp/client.go - HTTP-stream Transport                │
│  - POST /mcp/call        (JSON-RPC request)                   │
│  - GET  /mcp/stream       (HTTP-stream events)               │
└────────────────────────────────────────────────────────────────┘
                                                 │
                                                 ▼
┌────────────────────────────────────────────────────────────────┐
│                    WebHost MCP Server                         │
│  HTTP Server (port 9223)                                       │
│  ├── POST /mcp/call     → JSON-RPC 工具调用                    │
│  ├── GET  /mcp/stream   → HTTP-stream 长连接事件流              │
│  ├── GET  /mcp/sessions → Session 管理                         │
│  └── Health Check                                                │
│                                                                  │
│  ┌────────────────────────────────────────────────┐            │
│  │  macOS: SwiftUI + WebKit                      │            │
│  │  Windows: WPF + WebView2                     │            │
│  └────────────────────────────────────────────────┘            │
└────────────────────────────────────────────────────────────────┘
```

## 交互流程图

### 1. Session 创建与使用

```
Agent                      MCP Client              WebHost
  │                           │                      │
  │── web_session_create ────►│                      │
  │                           │── HTTP POST ────────►│
  │                           │◄── sessionId ────────│
  │◄── {sessionId} ──────────│                      │
  │                           │                      │
  │   (观察模式 - 透明蒙层)     │                      │
  │                           │                      │
  │── page_open ─────────────►│                      │
  │                           │── navigate ─────────►│ WebView 加载页面
  │                           │◄── complete ──────────│
  │◄── {success} ─────────────│                      │
  │                           │                      │
  │── script_run ─────────────►│                      │
  │                           │── evaluate ─────────►│
  │                           │◄── value ────────────│
  │◄── {value} ───────────────│                      │
  │                           │                      │
```

### 2. 事件流与心跳

```
Agent                      MCP Client              WebHost
  │                           │                      │
  │── GET /stream?since=0 ────►│                      │
  │                           │                      │ ◄── HTTP-stream 长连接
  │                           │◄── web/console ───────│
  │◄── console.log(...) ──────│                      │
  │                           │                      │
  │                           │◄── web/navigation ───│
  │◄── loading... ───────────│                      │
  │                           │                      │
  │         ... 30s ...       │                      │
  │                           │◄── web/ping ─────────│
  │◄── {t:xxx} (心跳) ───────│                      │
  │                           │                      │
```

### 3. 交互模式切换

```
Agent                      WebHost                  WebView
  │                           │                       │
  │   (当前: 观察模式)          │                       │
  │   透明蒙层遮盖 + JS拦截     │                       │
  │                           │                       │
  │── web_set_interactive=true►│                       │
  │                           │── 移除蒙层 ───────────►│
  │                           │── 恢复焦点 ───────────►│
  │                           │── 移除JS拦截 ─────────►│
  │◄── {success} ─────────────│                       │
  │                           │                       │
  │   (用户可以操作页面)        │                       │
  │   用户输入验证码...         │                       │
  │   用户点击确认...           │                       │
  │                           │                       │
  │── web_set_interactive=false►│                      │
  │                           │── 添加蒙层 ───────────►│
  │                           │── 注入JS拦截 ─────────►│
  │◄── {success} ─────────────│                       │
  │                           │                       │
  │   (恢复: 观察模式)          │                       │
```

### 4. 弹窗捕获

```
WebView                    WebHost                  Agent
  │                           │                      │
  │  用户点击按钮触发JS弹窗     │                      │
  │── confirm("确认删除?") ───►│                      │
  │                           │── web/dialog ────────►│
  │                           │◄── {type:confirm,    │     等待Agent响应
  │                           │     message:"...",   │
  │                           │     dialogId:"xxx"}  │
  │                           │                      │
  │◄── completionHandler(false)│  (默认拒绝)           │
  │   (弹窗等待)              │                       │
  │                           │◄── web_dialog_response│
  │                           │    {dialogId:"xxx",   │
  │                           │     action:accept}   │
  │◄── completionHandler(true) │                      │
  │   (弹窗关闭)              │                       │
```

### 5. 断线重连

```
Agent                      WebHost
  │                           │
  │── GET /stream?since=100 ──►│  (首次连接)
  │◄── events(t>100) ─────────│
  │                           │
  │   (网络抖动，断线)          │
  │                           │
  │   (恢复连接)               │
  │── GET /stream?since=150 ──►│  (从断点恢复)
  │◄── events(t>150) ─────────│  (不会丢失事件)
```

### 6. 完整用户场景

```
用户场景: Agent 自动化登录后需要用户输入验证码

1. Agent 创建 Session (观察模式)
   Agent ──► web_session_create(interactive=false)
   WebHost ──► 创建 WKWebView + 透明蒙层

2. Agent 打开登录页
   Agent ──► page_open(url="https://login.example.com")
   WebHost ──► WebView 加载页面
   Agent ◄── {success:true}

3. Agent 执行登录脚本
   Agent ──► script_run(script="document.querySelector('#user').value='admin'")
   Agent ◄── {success:true}

4. 页面弹出验证码
   WebView ──► confirm("请输入验证码")
   WebHost ──► web/dialog 事件
   Agent ◄── {type:"confirm", message:"请输入验证码"}

5. Agent 切换为交互模式
   Agent ──► web_set_interactive(interactive=true)
   WebHost ──► 移除蒙层
   用户 ──► 输入验证码，点击确认

6. Agent 恢复自动化
   Agent ──► web_set_interactive(interactive=false)
   WebHost ──► 添加蒙层
   Agent ──► script_run(script="submitVerification()")

7. 登录完成，关闭 Session
   Agent ──► web_session_close
   WebHost ──► 关闭 WebView，清理资源
```

## 协议设计

### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/mcp/call` | JSON-RPC 请求 (工具调用) |
| GET | `/mcp/stream` | HTTP-stream 事件流 |
| GET | `/mcp/sessions` | 列出所有活跃 session |
| DELETE | `/mcp/sessions/{id}` | 关闭指定 session |
| GET | `/health` | 健康检查 |

### JSON-RPC 2.0 消息格式

#### 请求示例

```json
// 创建 Session
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{
  "name": "web_session_create",
  "arguments": {"viewport":{"width":1920,"height":1080}}
}}

// 导航
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{
  "name": "page_open",
  "arguments": {"sessionId":"sess_001","url":"https://example.com"}
}}

// 执行 JS
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
  "name": "script_run",
  "arguments": {"sessionId":"sess_001","script":"document.title"}
}}

// 截图
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
  "name": "page_screenshot",
  "arguments": {"sessionId":"sess_001","format":"png"}
}}
```

#### 响应示例

```json
// 成功
{"jsonrpc":"2.0","id":1,"result":{"success":true,"sessionId":"sess_001"}}

// 成功 - navigate
{"jsonrpc":"2.0","id":2,"result":{"success":true,"url":"https://example.com","title":"Example","status":"complete"}}

// 成功 - evaluate
{"jsonrpc":"2.0","id":3,"result":{"success":true,"value":"Example"}}

// 成功 - screenshot
{"jsonrpc":"2.0","id":4,"result":{"success":true,"data":"base64...","mimeType":"image/png"}}

// 错误
{"jsonrpc":"2.0","id":2,"error":{"code":-32002,"message":"Navigation timeout"}}
```

### HTTP-stream 事件流

```http
GET /mcp/stream?sessionId=sess_001

{"jsonrpc":"2.0","method":"web/console","params":{"t":1715923200,"msg":"..."}}
{"jsonrpc":"2.0","method":"web/navigation","params":{"t":1715923201,"url":"...","status":"complete"}}
{"jsonrpc":"2.0","method":"web/ping","params":{"t":1715923202}}
```

### 重连机制

```http
# 初始请求
GET /mcp/stream?sessionId=sess_001&since=0

# 断线后重连
GET /mcp/stream?sessionId=sess_001&since=1715923201
# 返回 t > 1715923201 的所有事件
```

### 心跳检测

- Server 每 30s 发 `web/ping`
- Client 60s 无消息则重连

## 工具列表

| Tool | 说明 |
|------|------|
| `web_session_create` | 创建浏览器 Session |
| `page_open` | 导航到 URL |
| `script_run` | 执行 JavaScript (万能操作) |
| `page_screenshot` | 截图 |
| `web_inject` | 注入 CSS/JS |
| `web_wait` | 等待元素出现 |
| `web_set_interactive` | 切换交互模式（观察/可操作） |
| `web_capabilities` | 获取能力列表 |
| `web_session_close` | 关闭 Session |

## 交互模式

### 观察模式 (默认 interactive=false)

- 添加透明蒙层 (BlockView) 拦截鼠标/键盘
- 注入 JS 阻止事件传播
- 用户只能看，不能操作

### 交互模式 (interactive=true)

- 移除透明蒙层
- 恢复 WebView 焦点
- 用户可以操作页面

### 弹窗捕获

| 弹窗类型 | 事件 |
|----------|------|
| alert/confirm/prompt | `web/dialog` |
| window.open | `web/popup` |

## 事件类型

| Method | 说明 |
|--------|------|
| `web/console` | 页面 console.log |
| `web/navigation` | 导航状态变化 |
| `web/error` | 页面 JS 错误 |
| `web/dialog` | JavaScript 弹窗 |
| `web/popup` | 新窗口打开 |
| `web/screenshot_progress` | 截图进度 |
| `web/ping` | 心跳保活 |

## Session 管理

### 生命周期

```
Agent → page_open → script_run → GET /stream → web_session_close
```

### 持久化

Session 信息存储到磁盘，重启后恢复。

### 配置

```yaml
mcp:
  servers:
    webhost:
      url: http://localhost:9223
      session:
        maxCount: 10
        idleTimeout: 5m
        storageDir: /tmp/webhost
```

## 错误码

| Code | Message | 说明 |
|------|---------|------|
| -32600 | Invalid Request | JSON-RPC 格式错误 |
| -32602 | Invalid Params | 参数验证失败 |
| -32000 | Session Not Found | session 不存在 |
| -32001 | Session Limit Exceeded | 超过最大 session 数 |
| -32002 | Navigation Timeout | 页面导航超时 |
| -32003 | Script Timeout | JS 执行超时 |

## 实现

### 目录结构

```
deps/
├── macos/
│   └── webhost/           # SwiftUI + WebKit
│       ├── Sources/
│       │   └── WebHost/
│       │       ├── main.swift
│       │       ├── Server/
│       │       ├── Browser/
│       │       └── Model/
│       └── project.yml
└── win/
    └── webhost/           # WPF + WebView2
        └── src/
            └── WebHost/
```

### macOS 实现

依赖: SwiftNIO, WebKit, SwiftUI

核心组件:
- `McpServer` - HTTP Server 处理 JSON-RPC
- `WebKitSession` - WKWebView 封装
- `BlockerView` - 透明蒙层
- `SessionManager` - Session 生命周期

### Windows 实现

依赖: Microsoft.Web.WebView2, ASP.NET Core

核心组件:
- `McpServer` - ASP.NET Core 处理 JSON-RPC
- `WebView2Session` - WebView2 封装
- `BlockerOverlay` - 透明蒙层 Border

## 交付标准

| 功能 | 验收条件 |
|------|---------|
| Session 创建/关闭 | 可以创建多个 session 并关闭 |
| 页面导航 | `page_open` 成功加载页面 |
| JS 执行 | `script_run` 返回结果 |
| 截图 | `page_screenshot` 返回 PNG |
| 交互模式 | `web_set_interactive` 切换蒙层 |
| 事件流 | `GET /mcp/stream` 返回事件 |
| 重连 | `?since=` 从断点恢复 |
| 心跳 | 每 30s 收到 `web/ping` |
| 能力检测 | `web_capabilities` 返回能力列表 |
| 弹窗捕获 | `web/dialog` 事件触发 |

## TODO

- [ ] **企业环境证书/代理**: 企业环境如何处理证书验证和代理配置？
<!-- last-modified: 2026-05-17 -->
