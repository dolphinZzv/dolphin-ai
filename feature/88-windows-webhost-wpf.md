# Feature: Windows WebHost WPF + WebView2 实现

- 提出时间: 2026-05-17
- 状态: proposed
- 描述: 基于 `design/modules/webhost.md` 实现 Windows 端的 WebHost 原生 UI 集成。使用 WPF + WebView2，通过 HTTP-stream + JSON-RPC 2.0 与 Dolphin Agent 通信，提供浏览器自动化能力（页面导航、JS执行、截图、交互模式切换、弹窗捕获等）。

## 来源
- 用户需求: "实现完整的 WPF WebHost"
- 约束: 不依赖 Visual Studio，纯 `dotnet build` 构建；低资源消耗；.NET 5.0 SDK

## 设计
见 `design/modules/windows-webhost.md`
