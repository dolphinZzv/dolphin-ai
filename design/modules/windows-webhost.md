# Windows WebHost 实现设计 (WPF + WebView2)

## 概述

基于 `design/modules/webhost.md` 的总体设计，本文件详细描述 Windows 端 WPF + WebView2 实现方案。

## 约束

| 约束 | 说明 |
|------|------|
| 构建工具 | 纯 CLI：`dotnet build`，不依赖 Visual Studio |
| .NET 版本 | .NET 5.0 SDK / 5.0 runtime |
| 外部依赖 | 仅 `Microsoft.Web.WebView2` 一个 NuGet 包 |
| HTTP 服务 | `System.Net.HttpListener`（内置，无额外依赖） |
| 运行环境 | Windows 10+，x64，已安装 WebView2 Runtime |
| 资源要求 | 低内存占用，支持旧机器 |

## 架构

```
┌─────────────────────────────────────────────────────────┐
│                  WebHost.exe (WPF App)                    │
│                                                          │
│  ┌──────────────┐    ┌────────────────────────────┐     │
│  │  McpServer    │    │     SessionManager          │     │
│  │  (HttpListener│◄──►│  ┌──────────────────────┐  │     │
│  │   :9223)      │    │  │  WebView2Session #1  │  │     │
│  │               │    │  │  ┌──────────┐       │  │     │
│  │  POST /mcp/call───►│  │  │ WebView2 ├───┐ │     │
│  │  GET  /mcp/stream  │  │  │ Blocker  │   │ │     │
│  │  GET  /health      │  │  └──────────┘   │ │     │
│  └──────────────┘    │  │                  │ │     │
│                      │  │   Hidden Window  │ │     │
│                      │  └──────────────────┘ │     │
│                      │  ┌──────────────────────┐  │     │
│                      │  │  WebView2Session #2  │  │     │
│                      │  └──────────────────────┘  │     │
│                      └────────────────────────────┘     │
└─────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. McpServer (`McpServer.cs`)

使用 `HttpListener` 实现轻量级 HTTP 服务器，监听 `http://localhost:9223`。

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 → `{"status":"ok"}` |
| `/mcp/call` | POST | JSON-RPC 2.0 请求 → 路由到 SessionManager |
| `/mcp/stream` | GET | HTTP-stream 事件流（SSE 格式） |
| `/mcp/sessions` | GET | 列出活跃 session |
| `/mcp/sessions/{id}` | DELETE | 关闭指定 session |

### 2. SessionManager (`SessionManager.cs`)

管理所有 WebView2 session 的生命周期。

- `ConcurrentDictionary<string, WebView2Session>` 存储活跃 session
- 最大 session 数: 10（可配置）
- 空闲超时: 5m（可配置）
- 生成 session ID: `sess_` + 8位随机 hex
- 线程安全，所有 WebView2 操作通过 WPF Dispatcher 封送

### 3. WebView2Session (`WebView2Session.cs`)

封装单个 WebView2 实例，运行在隐藏的 WPF 窗口中。

关键方法：
- `InitializeAsync()` — 创建隐藏窗口 + 初始化 WebView2
- `NavigateAsync(url)` — 导航到 URL
- `ExecuteScriptAsync(script)` — 执行 JS → 返回结果
- `TakeScreenshotAsync()` — 截图 → 返回 base64 PNG
- `SetInteractiveAsync(bool)` — 切换蒙层
- `InjectContentAsync(css, js)` — 注入 CSS/JS
- `WaitForElementAsync(selector, timeout)` — 等待元素
- `CloseAsync()` — 关闭 session

事件发布到 EventStream：
- `web/console` — console.log 输出
- `web/navigation` — 导航状态
- `web/error` — JS 错误
- `web/dialog` — alert/confirm/prompt 弹窗
- `web/ping` — 心跳（每 30s）

### 4. BlockerOverlay (`BlockerOverlay.xaml`)

透明蒙层，用于拦截鼠标/键盘输入。

- 全透明背景
- `IsHitTestVisible="True"` 拦截所有输入
- 可选半透明遮罩层（视觉指示）
- 通过 `Visibility` 切换显示/隐藏

### 5. EventStream (`EventStream.cs`)

管理每个 session 的事件队列和 HTTP-stream 连接。

- 事件存储：`ConcurrentQueue<JsonElement>` + `SemaphoreSlim` 做等待
- SSE 格式：`data: {...}\n\n`
- 心跳：每 30s 发送 `web/ping`
- 断线重连：`since=timestamp` 参数过滤事件

## 数据模型

### JSON-RPC 2.0 消息

```json
// 请求
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"page_open","arguments":{...}}}
// 响应
{"jsonrpc":"2.0","id":1,"result":{"success":true,"sessionId":"sess_abc123"}}
// 错误
{"jsonrpc":"2.0","id":1,"error":{"code":-32002,"message":"Navigation timeout"}}
```

### 事件流格式 (SSE)

```
data: {"jsonrpc":"2.0","method":"web/console","params":{"t":1715923200,"msg":"hello"}}

data: {"jsonrpc":"2.0","method":"web/ping","params":{"t":1715923202}}
```

## 线程模型

```
┌──────────────┐          ┌──────────────────┐
│  WPF UI      │   main   │  HttpListener    │
│  (STA 线程)  │◄────────►│  (线程池)        │
│              │ Dispatcher│                  │
│  WebView2    │ 封送调用  │  JSON-RPC 路由   │
│  操作        │          │  事件流发送      │
└──────────────┘          └──────────────────┘
```

- WPF 主线程（STA）：WebView2 创建与操作
- HTTP 线程池：请求解析、路由、非 UI 操作
- 所有 WebView2 调用通过 `Application.Current.Dispatcher.InvokeAsync()`
- 事件通过线程安全的 `ConcurrentQueue` + `SemaphoreSlim` 传递

## 错误处理

| 场景 | 行为 |
|------|------|
| Session 不存在 | 返回 JSON-RPC 错误码 -32000 |
| Session 数超限 | 返回 JSON-RPC 错误码 -32001 |
| 导航超时 | 默认 30s 超时，返回 -32002 |
| JS 执行超时 | 默认 10s 超时，返回 -32003 |
| WebView2 未初始化 | 等待初始化完成，超时返回错误 |
| HTTP 服务端口占用 | 启动时检测，日志警告 |

## 构建与运行

```powershell
# 构建
cd deps/win/webhost/src/WebHost
dotnet restore
dotnet build

# 运行
dotnet run
```

## 项目文件结构

```
deps/win/webhost/src/WebHost/
├── global.json               # SDK 版本锁定
├── WebHost.csproj            # 项目文件
├── App.xaml / App.xaml.cs    # 应用入口，启动 MCP 服务器
├── MainWindow.xaml / .cs     # 主窗口（常隐藏）
├── McpServer.cs              # HTTP 服务器 + JSON-RPC 处理
├── SessionManager.cs         # Session 生命周期管理
├── WebView2Session.cs        # WebView2 封装
├── BlockerOverlay.xaml / .cs # 透明蒙层
├── EventStream.cs            # 事件流管理
└── Models/
    ├── JsonRpcMessage.cs     # JSON-RPC 2.0 模型
    └── SessionInfo.cs        # Session 信息模型
```

<!-- last-modified: 2026-05-17 -->
