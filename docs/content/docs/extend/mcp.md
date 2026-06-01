---
title: MCP
weight: 2
---

MCP（Model Context Protocol）是 Dolphin 的插件化工具系统，支持动态加载外部工具。

不同传输层可能看到不同的工具集。例如在 DingTalk 传输层下，MCP 会注册文件上传工具。

## 查看工具

| 命令 | 说明 |
|------|------|
| `/mcp` | 列出当前传输层下所有可用的 MCP 工具 |

### 示例

```text
Loaded tools:
  FILE_UPLOAD — Upload a file to DingTalk and share it in the group chat
```
