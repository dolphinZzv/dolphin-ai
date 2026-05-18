# dolphin

<p align="center">
  <strong>文档：</strong> <a href="README.md">English</a> · <a href="README.zh.md">中文</a>
  <br>
  <strong>分发：</strong> <a href="https://github.com/dolphinZzv/dolphin">GitHub</a> · <a href="https://gitee.com/dolphinzzv/dolphindolphin">Gitee</a>
  <br>
  <strong>安装：</strong> <a href="docs/zh/INSTALL.zh.md">安装指南</a>
</p>

一个在你工作的地方随时待命的 AI agent —— 终端、邮件、聊天、SSH。它执行 shell 命令、操控浏览器、调度子 agent 并行工作、按你定义的计划自动运行任务。就像一个能干的队友，无论通过什么渠道都能对接。

## 为什么是 dolphin？

大多数 AI 编程工具把你锁在某个编辑器或 Web UI 里。写代码没问题，但实际工作远不止于此。你可能想在手机上用邮件问 agent 点事，或者让它每晚自动执行任务无需任何人碰键盘，或者 SSH 到服务器让上面的 agent 帮忙排查问题。

dolphin 不在乎你敲哪个门 —— 它都会应答。同一个 agent、同一套工具、同一份会话状态，无论走哪个传输层。

## 能做什么

**执行命令与自动化工作流。** shell 工具让它能访问文件系统、git，包管理器、构建工具 —— 所有你在终端里敲的东西。超时机制和可选的 allowlist 保证安全。

**操控浏览器。** 通过 CDP（Chrome DevTools Protocol）工具，它能打开页面、点击、填表、截图、提取数据。适合测试、抓取或自动化那些没有 API 的 web 任务。

**协调多个 agent。** 需要代码审查、安全审计和部署检查同时进行？协调器会把任务分发给专门的子 agent 并行执行。你可以定义常驻 agent 承担重复角色，也可以让协调器临时创建。

**按需学习技能。** 技能是教 agent 如何做特定事情的 markdown 文件 —— 代码审查模式、部署检查清单、数据库迁移步骤。agent 只在需要时加载所需内容，系统 prompt 保持精简。

**按计划执行。** 在项目里放一个 CRONTAB.md，agent 就会按 cron 计划运行任务 —— 每日摘要、每周维护，你需要什么节奏就什么节奏。结果会像其他 agent 输出一样出现在会话里。

**对接外部工具。** 任何 MCP 兼容的服务（数据库检查器、API 浏览器、代码检查器）都可以通过配置接入。agent 会自动发现可用工具并在恰当时使用。

## 连接方式

dolphin 支持五种传输协议，可以同时启用：

- **stdio** — 默认。运行 `./dolphin` 在终端聊天。首次运行会引导你设置职业画像和推荐工具。
- **SSH** — 远程连接。`ssh dolphin@host -p 2222`。同样的 agent 会话，终端界面。
- **MQTT** — 轻量级发布/订阅消息。适合嵌入式设备、聊天应用或事件驱动自动化。附带原生 macOS 客户端（Panda）。
- **Email** — 把命令作为邮件主题发送，收件箱收到回复。按可配置的时间间隔轮询 IMAP。
- **DingTalk** — 通过钉钉机器人接入，支持团队协作。基于钉钉 Stream 模式实现交互式命令和通知推送。

所有传输层共享同一个 agent 实例、工具和会话状态，可随意切换。

## 快速开始

### 一键启动

```bash
go build -o dolphin .

# DeepSeek 示例（中国地区可直接访问）
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="deepseek-v4-flash"
export DZ_LLM_BASE_URL="https://api.deepseek.com"
export DZ_LLM_TYPE="openai"

# MiniMax M2.7 示例
# export DZ_LLM_API_KEY="sk-..."
# export DZ_LLM_MODEL="MiniMax-M2.7"
# export DZ_LLM_BASE_URL="https://api.minimax.chat/v1"
# export DZ_LLM_TYPE="openai"

# 智谱 GLM-5.1 示例
# export DZ_LLM_API_KEY="sk-..."
# export DZ_LLM_MODEL="glm-5.1"
# export DZ_LLM_BASE_URL="https://open.bigmodel.cn/api/paas/v4"
# export DZ_LLM_TYPE="openai"

# 通义千问 Qwen3.6 示例
# export DZ_LLM_API_KEY="sk-..."
# export DZ_LLM_MODEL="qwen3.6-max-preview"
# export DZ_LLM_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"
# export DZ_LLM_TYPE="openai"

./dolphin
```

### 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `DZ_LLM_API_KEY` | **是** | — | LLM API 密钥 |
| `DZ_LLM_MODEL` | **是** | — | 模型名称（如 `deepseek-v4-flash`、`glm-5.1`、`MiniMax-M2.7`、`qwen3.6-max-preview`、`kimi-k2.6`） |
| `DZ_LLM_BASE_URL` | **是** | — | API 基础地址（如 `https://api.deepseek.com`、`https://open.bigmodel.cn/api/paas/v4`） |
| `DZ_LLM_TYPE` | **是** | `openai` | 提供商类型：`openai` 或 `anthropic`。中国地区建议使用兼容 OpenAI 接口的服务商（DeepSeek、通义千问等） |

### 中国地区推荐模型

| 服务商 | 模型 | 接口地址 | 接入方式 |
|--------|------|----------|----------|
| **DeepSeek** | `deepseek-v4-flash` | `https://api.deepseek.com` | OpenAI 兼容 |
| **MiniMax** | `MiniMax-M2.7` | `https://api.minimax.chat/v1` | OpenAI 兼容 |
| **智谱 GLM** | `glm-5.1` | `https://open.bigmodel.cn/api/paas/v4` | OpenAI 兼容 |
| **通义千问** | `qwen3.6-max-preview` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | OpenAI 兼容 |
| **Kimi** | `kimi-k2.6` | `https://api.moonshot.ai/v1` | OpenAI 兼容 |

以上服务商均兼容 OpenAI 接口格式，设置 `DZ_LLM_TYPE=openai` 即可使用。

### 首次运行

首次运行 dolphin 会逐步引导设置：

1. **职业画像** — 选择角色（前端、后端、DevOps、数据等）。agent 会推荐匹配的技能和 MCP 工具。
2. **SYSTEM.md** — 可选生成系统信息文件，让 agent 了解你的操作系统、Shell 和工作环境。
3. **配置文件** — 可选生成 `.dolphin/config.yaml`，预填所有默认值并附带注释。

一切都在终端内交互完成，数据不会离开你的机器。

以后重新运行向导：`./dolphin setup`

### 配置

配置文件位于 `.dolphin/config.yaml`（项目级）或 `~/.dolphin/config.yaml`（用户级）。项目配置覆盖用户配置。所有设置都有合理默认值 —— 只需 API 密钥即可运行。

```bash
# DeepSeek（中国推荐）
export DZ_LLM_API_KEY="sk-..."
export DZ_LLM_MODEL="deepseek-v4-flash"
export DZ_LLM_BASE_URL="https://api.deepseek.com"
./dolphin
```

## 源码编译

dolphin 支持 **Linux**、**macOS** 和 **Windows**（arm64 和 x86_64）。完整安装方式（预编译二进制、go install 等）见 [INSTALL.zh.md](docs/zh/INSTALL.zh.md)。

快速参考：

```bash
git clone https://github.com/dolphinZzv/dolphin.git
cd dolphin

# Linux / macOS
make build                  # 开发版本（版本号 = dev）
make build VERSION=v1.0.0   # 发布版本

# Windows（PowerShell）
go build -o dolphin.exe .   # 开发版本

# Windows（通过 Chocolatey/winget 安装 make 后）
make build                  # 开发版本
```

详细文档见 `design/` 目录。

## 设计哲学

dolphin 基于以下信念构建：

- **人在哪，agent 就在哪。** agent 不应该需要特殊的 UI，它应该接入已经在用的工具和渠道。
- **渐进式披露。** 优先展示最相关的工具和技能，让 LLM 在需要时自行搜索更多，不淹没上下文窗口。
- **本地优先，尊重隐私。** 职业画像、SYSTEM.md、会话文件 —— 全部本地存储。除了你配置的 LLM API 调用，没有任何数据外发。
- **可恢复设计。** 会话持久化到磁盘。agent 崩溃或关机后可以接着上次继续。日志轮转但不会消失。
- **可测试、可观测。** 结构化日志、Prometheus 指标、pprof 端点，以及在 CI 中强制执行竞态检测和覆盖率门槛的测试套件。

---

**联系方式：** [dolphin@siciv.space](mailto:dolphin@siciv.space)
