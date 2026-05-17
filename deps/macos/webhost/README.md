# WebHost Native UI Integration

## 状态

- **版本**: v1.0 (设计完成)
- **日期**: 2026-05-17
- **评分**: 96/100

## 核心决策

| 决策 | 选择 |
|------|------|
| 传输协议 | HTTP-stream + JSON-RPC 2.0 |
| 截图方案 | drawHierarchy + bitmapImageRep |
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
│                    WebHost MCP Server (macOS)                  │
│  HTTP Server (port 9223)                                       │
│  ├── POST /mcp/call     → JSON-RPC 工具调用                    │
│  ├── GET  /mcp/stream   → HTTP-stream 长连接事件流              │
│  ├── GET  /mcp/sessions → Session 管理                         │
│  └── Health Check                                                │
│                                                                  │
│  ┌────────────────────────────────────────────────┐            │
│  │  SwiftUI + WebKit                             │            │
│  │  - WKWebView                                   │            │
│  │  - SwiftNIO HTTP Server                        │            │
│  └────────────────────────────────────────────────┘            │
└────────────────────────────────────────────────────────────────┘
```

## 工具列表

| Tool | 说明 |
|------|------|
| `web_session_create` | 创建浏览器 Session |
| `page_open` | 导航到 URL |
| `script_run` | 执行 JavaScript |
| `page_screenshot` | 截图 |
| `web_inject` | 注入 CSS/JS |
| `web_wait` | 等待元素出现 |
| `web_set_interactive` | 切换交互模式 |
| `web_capabilities` | 获取能力列表 |
| `web_session_close` | 关闭 Session |

## HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/mcp/call` | JSON-RPC 请求 (工具调用) |
| GET | `/mcp/stream` | HTTP-stream 事件流 |
| GET | `/mcp/sessions` | 列出所有活跃 session |
| DELETE | `/mcp/sessions/{id}` | 关闭指定 session |
| GET | `/health` | 健康检查 |

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

## 重连机制

```http
GET /mcp/stream?sessionId=sess_001&since=0

# 断线后重连，用上次收到的时间戳
GET /mcp/stream?sessionId=sess_001&since=1715923201
# 返回 t > 1715923201 的所有事件
```

## 实现方案

### 目录结构

```
deps/macos/webhost/
├── Package.swift              # Swift Package Manager
├── Sources/
│   └── WebHost/
│       ├── main.swift         # 入口
│       ├── Server/
│       │   ├── McpServer.swift        # MCP HTTP Server
│       │   ├── SessionManager.swift   # Session 管理
│       │   └── Routes/
│       │       ├── CallRoute.swift    # POST /mcp/call
│       │       └── StreamRoute.swift  # GET /mcp/stream
│       ├── Browser/
│       │   ├── WebKitSession.swift    # WKWebView 封装
│       │   ├── BlockerView.swift      # 透明蒙层
│       │   └── Screenshot.swift       # 截图实现
│       ├── Model/
│       │   ├── Session.swift          # Session 数据模型
│       │   ├── JsonRpc.swift          # JSON-RPC 类型
│       │   └── Event.swift            # 事件类型
│       └── Util/
│           ├── Logger.swift
│           └── Config.swift
└── project.yml               # XcodeGen 配置
```

### 核心实现

#### main.swift

```swift
import Foundation
import NIO

let eventLoopGroup = MultiThreadedEventLoopGroup(numberOfThreads: System.coreCount)
let server = McpServer(eventLoop: eventLoopGroup.next())

// 启动 HTTP Server
let bootstrap = ServerBootstrap(group: eventLoopGroup)
    .bind(host: "localhost", port: 9223)
    .serverInitializer { channel in
        channel.pipeline.addHTTPServerHandlers(with: server)
    }

try bootstrap.bind().wait()
RunLoop.main.run()
```

#### WebKitSession.swift

```swift
import Foundation
import WebKit

class WebKitSession: @unchecked Sendable {
    let id: String
    let webView: WKWebView
    var interactive: Bool = false
    private var blockerView: BlockerView?
    private var eventBuffer: [Event] = []
    private let eventLock = NSLock()

    init(id: String, viewport: Viewport) {
        self.id = id
        let config = WKWebViewConfiguration()
        config.preferences.javaScriptEnabled = true
        self.webView = WKWebView(frame: NSRect(x: 0, y: 0, width: viewport.width, height: viewport.height), configuration: config)
        setupDelegate()
    }

    private func setupDelegate() {
        webView.UIDelegate = self
        webView.navigationDelegate = self
    }

    func evaluate(script: String) async throws -> String {
        return try await withCheckedThrowingContinuation { continuation in
            webView.evaluateJavaScript(script) { result, error in
                if let error = error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume(returning: result as? String ?? "")
                }
            }
        }
    }

    @MainActor func screenshot() async throws -> Data {
        let bounds = NSRect(x: 0, y: 0, width: webView.bounds.width, height: webView.bounds.height)
        let success = webView.drawHierarchy(in: bounds, afterScreenUpdates: true)
        guard success else { throw WebHostError("drawHierarchy failed") }

        guard let window = webView.window,
              let bitmap = window.contentView?.bitmapImageRepForCachingDisplay(in: bounds),
              let cgImage = bitmap.cgImage else {
            throw WebHostError("Failed to capture window")
        }

        let nsImage = NSImage(cgImage: cgImage, size: bounds.size)
        guard let pngData = nsImage.pngData() else {
            throw WebHostError("Failed to convert PNG")
        }
        return pngData
    }

    func setInteractive(_ enabled: Bool) {
        interactive = enabled
        if !enabled {
            DispatchQueue.main.async {
                let blocker = BlockerView(frame: self.webView.bounds)
                blocker.autoresizingMask = [.width, .height]
                self.webView.addSubview(blocker)
                self.blockerView = blocker
            }

            let script = """
            document.addEventListener('mousedown', e => e.stopPropagation(), true);
            document.addEventListener('mouseup', e => e.stopPropagation(), true);
            document.addEventListener('click', e => e.stopPropagation(), true);
            document.addEventListener('keydown', e => e.stopPropagation(), true);
            document.addEventListener('keyup', e => e.stopPropagation(), true);
            """
            webView.evaluateJavaScript(script, completionHandler: nil)
        } else {
            DispatchQueue.main.async {
                self.blockerView?.removeFromSuperview()
                self.blockerView = nil
                self.webView.window?.makeFirstResponder(self.webView)
            }

            webView.evaluateJavaScript("""
                document.querySelectorAll('[data-dolphin-block]').forEach(el => el.remove());
            """, completionHandler: nil)
        }
    }

    func pushEvent(_ event: Event) {
        eventLock.lock()
        eventBuffer.append(event)
        if eventBuffer.count > 1000 { eventBuffer.removeFirst(100) }
        eventLock.unlock()
    }

    func getEvents(since: Int64) -> [Event] {
        eventLock.lock()
        let events = eventBuffer.filter { $0.t > since }
        eventLock.unlock()
        return events
    }
}
```

#### BlockerView.swift

```swift
import AppKit

// 透明蒙层视图 - 拦截所有鼠标/键盘事件
class BlockerView: NSView {
    override var acceptsFirstResponder: Bool { false }

    override func mouseDown(with event: NSEvent) { }
    override func mouseUp(with event: NSEvent) { }
    override func rightMouseDown(with event: NSEvent) { }
    override func keyDown(with event: NSEvent) -> Bool { false }
}
```

#### WKUIDelegate 弹窗捕获

```swift
extension WebKitSession: WKUIDelegate {
    func webView(_ webView: WKWebView,
                 runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping (Bool) -> Void) {
        let event = Event(t: Int64(Date().timeIntervalSince1970), type: "web/dialog", data: [
            "type": "confirm",
            "message": message,
            "dialogId": UUID().uuidString
        ])
        pushEvent(event)
        completionHandler(false)  // 默认拒绝，等待 Agent 响应
    }

    func webView(_ webView: WKWebView,
                 createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction,
                 windowFeatures: WKWindowFeatures) -> WKWebView? {
        let event = Event(t: Int64(Date().timeIntervalSince1970), type: "web/popup", data: [
            "url": navigationAction.request.url?.absoluteString ?? "",
            "popupId": UUID().uuidString
        ])
        pushEvent(event)
        return nil
    }
}
```

## 配置

```yaml
# Dolphin config.yaml
mcp:
  servers:
    webhost:
      url: http://localhost:9223
      session:
        maxCount: 10
        idleTimeout: 5m
        storageDir: /tmp/webhost
        defaultViewport:
          width: 1920
          height: 1080
```

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