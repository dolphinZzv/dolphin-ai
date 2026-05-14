---
title: MCP 与 Skills
description: 通过外部 MCP 服务器和可加载技能扩展 Dolphin 的能力
slug: mcp-skills
weight: 30
---

Dolphin 通过两个互补的系统扩展能力：**MCP 服务器**（模型上下文协议）和 **Skills**（领域知识包）。

---

## MCP 服务器

MCP 服务器暴露 agent 可以调用的工具 — Shell 命令、浏览器自动化、邮件、问题追踪器或任何自定义服务。MCP 服务器的工具会出现在 agent 的工具列表中，LLM 可以在正常对话中调用。

### 内置 MCP 工具

Dolphin 内置了以下 MCP 工具：

| 工具 | 配置键 | 说明 |
|------|--------|------|
| Shell | `mcp.shell` | 本地执行 Shell 命令 |
| CDP 浏览器 | `mcp.cdp` | 通过 Chrome DevTools Protocol 进行浏览器自动化 |
| 邮件 | `mcp.email` | 发送、搜索和读取邮件 |
| Webhook | `mcp.webhook` | 向外部服务发送 HTTP 请求 |

这些工具可以独立开关和配置。详细配置参数参见[配置参考]({{< relref "docs/config" >}})。

### 外部 MCP 服务器

支持三种传输类型连接第三方 MCP 服务器：

**stdio** — 以子进程方式启动服务器：

```yaml
mcp:
  servers:
    filesystem:
      type: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
```

**sse** — 连接到远程 SSE 流（[chick](https://github.com/dolphinv/chick) 等服务器使用此方式）：

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

**http-stream** — 类似 SSE，使用 chunked transfer encoding：

```yaml
mcp:
  servers:
    my-server:
      type: http-stream
      url: "https://mcp.example.com/stream"
```

### 加载流程

Dolphin 启动时：

1. 逐个连接已配置的服务器，进行 MCP 握手（`initialize` → `notifications/initialized`）。
2. 通过 `tools/list` 发现工具，以服务器名作为前缀注册（如 `chick:search_issues`）。
3. 如果某个服务器启动失败，会输出警告并跳过——不影响其他服务器的加载。
4. 工具定义（名称、描述、输入 schema）会在每轮对话时注入 LLM 上下文。

### 故障排查

- **服务器连接失败**：检查 URL、网络访问和认证 headers。
- **工具 schema 为空**：服务器可能需要更新的 MCP 协议版本。Dolphin 发送的版本号是 `2024-11-05`。
- **调用报 "jsonrpc error"**：通常是服务端参数校验错误 — 检查该工具的参数 schema。
- **查看已加载的工具**：启动 Dolphin 后查看 `Loaded MCP tools:` 行，或输入 `/status`。

---

## Skills（技能）

Skills 是包含领域专业知识、agent 按需加载的 markdown 文件。可以把它们理解为"按需注入的系统提示词"——当你让 agent 执行 `load_skill react-best-practices` 时，React 相关的模式和知识就会注入到下一轮对话的上下文中。

### 技能文件格式

每个技能是一个 `.md` 文件，可包含 YAML frontmatter：

```markdown
---
name: react-best-practices
description: React 最佳实践 — Hooks、状态管理、性能优化
---

# React 最佳实践

## Hooks

- 传递给子组件的事件处理函数使用 `useCallback` 包裹
- 使用函数式状态更新避免闭包陷阱：`setCount(c => c + 1)`

## 状态管理

- 状态逻辑复杂时优先使用 `useReducer` 而非 `useState`
- ...
```

如果省略 frontmatter，文件名（去掉 `.md` 后缀）作为技能名，第一个标题作为描述。

### 配置

```yaml
skills:
  dir: .dolphin/skills        # 存放技能 .md 文件的目录
  max_top: 10                 # 系统提示中显示的排名靠前技能数量
  repos:                      # 社区技能清单仓库
    - dolphinv/skills
```

- **`dir`**：启动时扫描 `.md` 文件的目录。文件变更时热重载。
- **`max_top`**：按使用次数排序，前 N 个技能会列在系统提示中以便发现。
- **`repos`**：包含 `skills.json` 清单的 GitHub 仓库。技能会自动下载并合并到 `dir` 中。

### 使用技能

在会话中使用以下命令：

| 命令 | 作用 |
|------|------|
| `/skills` | 列出所有可用的技能 |
| `load_skill <name>` | 将指定技能加载到当前上下文 |

也可以让 agent 自行搜索和加载——LLM 可以在需要领域知识时将 `search_skills` 和 `load_skill` 作为工具调用。

排名靠前的技能（按调用次数，最多 `max_top` 个）会显示在每次系统提示中方便发现：

```
Available Skills
Skills are specialized capabilities you can load on demand with load_skill.

  react-best-practices — React 最佳实践 — Hooks、状态管理、性能优化
  backend-golang — Go 后端开发 — Gin/GRPC/微服务
  ...
```

### 编写自定义技能

1. 在技能目录中创建 `.md` 文件：

   ```bash
   mkdir -p .dolphin/skills
   ```

2. 编写包含 frontmatter 和正文的技能文件。正文会在加载时注入到 LLM 上下文，请保持内容聚焦：

   ```markdown
   ---
   name: my-api-conventions
   description: 团队 REST API 规范 — 错误码、认证、分页
   ---

   # API 规范

   - 错误格式：`{"error": {"code": "...", "message": "..."}}`
   - 认证方式：`Authorization` header 中传 Bearer token
   - 分页方式：`?page=N&limit=M`，响应体包含 `total` 字段
   ```

3. 技能会在下次启动时自动检测到，或文件变更后被热重载。

### 社区技能

设置 `skills.repos` 从社区清单仓库获取技能。清单是一个 `skills.json` 文件，列出所有可用技能：

```json
{
  "version": "1.0",
  "description": "dolphin 官方技能仓库",
  "repo_url": "https://github.com/dolphinv/skills",
  "tools": [
    {
      "name": "frontend-expert",
      "description": "前端专家技能 - 精通 React/Vue/Angular 等现代前端框架",
      "url": "https://github.com/dolphinv/skills/blob/main/frontend-expert/"
    }
  ]
}
```

`tools` 中的每个条目指向一个可下载并加载的技能。

### 热重载

技能目录会被监控文件变更。当你编辑目录中的 `.md` 文件时，管理器会在 5 秒内自动重载，无需重启。

---

## MCP + Skills 结合使用

MCP 工具和 Skills 协同工作：

- **MCP 工具**赋予 agent 新的*行动能力*（调用 API、执行命令、浏览网页）。
- **Skills**赋予 agent 新的*知识储备*（最佳实践、编码规范、领域上下文）。

示例工作流：

1. 配置 chick MCP 服务器用于跟踪 issues。
2. 创建一个包含团队 issue 分类规范的技能文件。
3. Agent 加载技能、学习你的分类规则，然后用 `chick:search_issues` 和 `chick:transition_issue` 按规则处理 issue。
