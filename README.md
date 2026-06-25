# Dolphin-AI

[![CI](https://github.com/dolphinZzv/dolphin-ai/actions/workflows/ci.yml/badge.svg)](https://github.com/dolphinZzv/dolphin-ai/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/dolphinZzv/dolphin-ai/branch/main/graph/badge.svg)](https://codecov.io/gh/dolphinZzv/dolphin-ai)

你卓尔不凡

**Dolphin** 是一个拥有自我进化能力的终端 AI 伙伴。它维护一个 git 版本化的知识库（Brain），在后台分析对话模式并**自动改进自己的指令和知识**，越用越聪明。

## 特性

### 🧠 Brain — Git 版本化知识库
对话中积累的知识、偏好、命令和技能持久化在一个 git 仓库中。支持 push/pull 在设备间同步，LLM 在每次对话中自动加载。越用越有「你」的风格。

### 🌙 Dream — 离线自我编辑
你离开 20 分钟后，Dream 自动启动：扫描所有新对话 → 发现纠正信号和重复模式 → 调用 LLM 精炼 Brain 内容 → git 分支提交 → 等你审核或自动合并。修错、去重、淘汰，全自动。

### 🖥️ TUI — 功能完整的终端界面
Bubble Tea 构建的原生 TUI。实时流式输出、思考链折叠、工具调用可视化、鼠标文本选择 + 复制、滑动日志、队列面板、快捷切换侧栏。全键盘操作，零鼠标依赖。

### 🔗 多传输通道
一个 agent 实例可同时服务多个通道：**TUI**（终端交互）、**WeWork** / **DingTalk**（企业 IM）、**Email**（SMTP/IMAP）、**Panda**（自定义协议）、**A2A**（Agent-to-Agent）。每条消息自动绑定对应会话。

### 🔧 MCP 生态
原生支持 Model Context Protocol。HTTP/SSE 和 Stdio 两种传输，所有 MCP 服务异步加载不阻塞启动。内置 `mcp_search` 和 `mcp_load` 工具，运行时可动态接入新 MCP 服务。

### 📦 完整工具链
Shell 命令、文件读写、Brain CRUD、技能管理、工作流引擎、定时任务、订阅推送——全部以工具形式暴露给 LLM。权限系统支持按工具粒度的 always/once/deny，内置危险命令拦截。

### 🌍 多模型 / 多语言
支持 OpenAI、Anthropic、DeepSeek 等多厂商。按模型粒度配置 temperature、reasoning_effort、thinking、自定义 HTTP headers。界面支持中英文双语，一键切换。

### 🔄 会话管理
多会话独立上下文。自动 compaction 在接近上下文窗口时触发，用 LLM 生成摘要替代旧消息。支持 dump 导出、switch 切换、pause/continue 暂停恢复。

---

## 快速开始

一键安装：

```shell
curl -fsSL https://raw.githubusercontent.com/dolphinZzv/dolphin-ai/main/install.sh | bash
```

或通过 Go 安装：

```shell
go install github.com/dolphinZzv/dolphin-ai/cmd/dolphin@latest
```

创建 `config.yaml`：

```yaml
llm:
  deepseek_anthropic:
    provider: deepseek
    api_type: anthropic
    api_key: "sk-xxx"
    base_url: "https://api.deepseek.com/anthropic"
    models:
      - name: deepseek-v4-pro
      - name: deepseek-v4-flash
```

启动：

```shell
dolphin
```

如需加载自定义配置文件：

```shell
dolphin --config /path/to/config.yaml
```

---

## 命令

所有命令以 `/` 开头，在 TUI 输入框中输入。

### 会话管理

| 命令 | 说明 |
|------|------|
| `/session status` | 当前会话统计（轮数、token、工具调用） |
| `/session switch <id>` | 切换活跃会话 |
| `/session new` | 创建新会话 |
| `/session dump [id]` | 导出会话最后轮次的 LLM 请求/响应 JSON |
| `/session compaction` | 手动压缩当前会话上下文 |
| `/session pause` | 暂停当前轮次 |
| `/session continue` | 继续暂停的轮次 |
| `/session stop` | 停止当前轮次 |
| `/dump` | `/session dump` 的别名 |
| `/compaction` | `/session compaction` 的别名 |

### AI 自改进

| 命令 | 说明 |
|------|------|
| `/dream now` | 立即触发一次 self-edit 扫描 |
| `/dream status` | 查看 dream 运行统计和效果 |
| `/dream preview` | 预览将要编辑的内容（不写入） |
| `/dream review` | 审核待处理的 dream 分支 |
| `/dream accept [N]` | 接受 dream 编辑（cherry-pick 或全部） |
| `/dream reject [N]` | 拒绝 dream 编辑 |
| `/dream diff [N]` | 查看单条 dream commit 的 diff |
| `/dream revert <id>` | 回滚指定 dream 的 merge commit |

### 模型与配置

| 命令 | 说明 |
|------|------|
| `/models` | 列出所有可用模型 |
| `/models use <model>` | 切换当前使用的模型 |
| `/config [key]` | 查看或搜索当前配置 |
| `/limit` | 查看 LLM 用量和限制状态 |
| `/limit reset [target]` | 重置 LLM 用量计数器 |
| `/lang list` | 列出可用语言 |
| `/lang use <code>` | 切换界面语言 |
| `/context [all\|name]` | 查看系统 prompt 上下文模块 |
| `/version` | 显示版本信息 |

### Brain（知识库）

| 命令 | 说明 |
|------|------|
| `/brain push` | 将 brain 提交推送到远程 |
| `/brain pull` | 从远程拉取 brain 更新 |
| `/brain set url <url>` | 设置 brain 远程仓库地址 |
| `/push` | `/brain push` 的别名 |
| `/pull` | `/brain pull` 的别名 |
| `/commands` | 列出 brain 中的自定义命令 |
| `/commands list` | 格式化的命令清单 |
| `/commands show <name>` | 查看命令详情和内容 |
| `/script` | 管理 brain 脚本（list/show/create/delete） |
| `/subscription` | 管理 brain 订阅（list/show/enable/disable） |
| `/scheduler` | 查看定时任务状态 |
| `/skills` | 管理技能（list/search/upsert/delete/load） |

### MCP 与工具

| 命令 | 说明 |
|------|------|
| `/mcp` | 列出所有注册的 MCP 服务器和工具 |
| `/mcp search <query>` | 搜索 MCP 服务目录 |
| `/mcp enable <source>` | 启用工具源 |
| `/mcp disable <source>` | 禁用工具源 |

### 队列

| 命令 | 说明 |
|------|------|
| `/queue` | 查看当前任务队列 |
| `/queue pop [index]` | 移除队列中的指定任务 |

### TUI 快捷命令

| 命令 | 说明 |
|------|------|
| `/tools` | 切换工具调用显示 |
| `/thinking` | 切换思考链显示 |
| `/windows` | 切换侧边状态面板 |
| `/exit` | 退出程序 |
