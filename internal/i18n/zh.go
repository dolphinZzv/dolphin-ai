package i18n

var zhMessages = map[string]string{
	KeyChoice:            "选择",
	KeySkills:            "技能",
	KeyMCP:               "MCP 工具",
	KeyInstallHint:       "要安装，请将工具添加到你的 skill/MCP 仓库或配置中。",
	KeyToolsInstalled:    "工具已安装。技能和 MCP 服务器立即可用。",
	KeyConfigRestrictive: "限制模式（推荐用于安全场景）",
	KeyRestrictiveHint:   "配置已应用安全加固。可手动修改后重启。",
	KeySystemMDPrompt:    "是否自动生成 SYSTEM.md？包含系统信息（操作系统、Shell、CPU 数量）",
	KeySystemMDExplain:   "它会在每次会话中注入，帮助 AI 了解你的运行环境。",
	KeySystemMDYes:       "是",
	KeySystemMDNo:        "跳过",
	KeySystemMDSkipped:   "已跳过。你可以稍后手动创建 ~/.dolphin/SYSTEM.md。",
	KeySystemMDGenerated: "已生成",
	KeySystemMDContent:   "系统环境",
	KeyConfigPrompt:      "是否自动生成 .dolphin/config.yaml？包含所有配置项及中文注释。",
	KeyConfigExplain:     "它会创建一个带完整注释的配置文件，方便你自定义传输层、工具、LLM 等。",
	KeyConfigYes:         "是",
	KeyConfigNo:          "跳过",
	KeyConfigSkipped:     "已跳过。将使用默认值。你可以稍后手动创建 .dolphin/config.yaml。",
	KeyConfigGenerated:   "配置文件已生成",

	// Coordinator interaction
	KeyCoordReady:          "dolphin ai 已就绪（输入 /help 查看命令）\n",
	KeyHelpHeader:          "命令：",
	KeyHelpExit:            "  /exit          - 退出",
	KeyHelpHelp:            "  /help         - 显示帮助",
	KeyHelpAgents:          "  /agents       - 列出可用代理及其状态",
	KeyHelpSkills:          "  /skills       - 列出可用技能（/skills help 查看子命令）",
	KeyHelpCommands:        "  /commands     - 用户自定义命令（/commands help 查看子命令）",
	KeyHelpCancel:          "  /cancel       - 取消所有运行中的任务",
	KeyHelpCancelID:        "  /cancel <id>  - 取消指定 ID 的任务",
	KeyHelpMCP:             "  /mcp          - 列出所有 MCP 工具",
	KeyHelpStatus:          "  /status       - 显示当前状态",
	KeyHelpSessions:        "  /sessions     - 查看历史会话",
	KeyHelpCron:            "  /crontab      - 查看定时任务",
	KeyHelpModel:           "  /model [name] - 列出或切换 LLM 提供商",
	KeyHelpTopMCP:          "常用 MCP 工具（按使用次数，使用 search_mcp_tools 查找更多）：",
	KeyHelpSkillsAvail:     "\n技能：%d 个可用（使用 /skills 列出，search_skills 查找）",
	KeyNoAgents:            "未配置代理。",
	KeyNoAgentsHint:        "在 .dolphin/agents/<name>/agent.yaml 中创建代理",
	KeyAgentHeader:         "%-16s %-10s %-6s %s",
	KeySkillsNotAvail:      "技能系统不可用。",
	KeyNoSkills:            "未找到技能。",
	KeyNoSkillsHint:        "将 .md 文件添加到 .dolphin/skills/",
	KeySkillHeader:         "%-20s %-8s %s",
	KeySkillSearchHint:     "使用 search_skills 查找技能，load_skill 加载技能。",
	KeySkillNewCreated:     "已在 %s 中创建技能 %q。编辑该文件以自定义内容。",
	KeySkillNewError:       "创建技能失败：%v",
	KeySkillNewUsage:       "用法：/skills new <名称>  — 在技能目录中创建技能模板",
	KeySkillDeleteUsage:    "  /skills delete <名称>  - 删除技能",
	KeySkillDeleteDone:     "技能 %q 已删除。",
	KeySkillDeleteFail:     "删除技能 %q 失败：%v",
	KeySkillShowUsage:      "  /skills show <名称>    - 查看技能内容",
	KeySkillShowFail:       "技能 %q 未找到。",
	KeySkillShowHeader:     "--- %s ---",
	KeyCommandsNotAvail:    "命令系统不可用。",
	KeyNoCommands:          "没有用户自定义命令。",
	KeyNoCommandsHint:      "将 .md 文件添加到 .dolphin/commands/",
	KeyCommandHeader:       "%-20s  %s",
	KeyCommandRunHint:      "输入 /<命令名> 来运行命令，可选带参数。",
	KeyCmdNewUsage:         "用法：/commands new <名称> [描述]  — 创建命令模板",
	KeyCmdNewCreated:       "已在 %s 中创建命令 %q。编辑该文件以自定义内容。",
	KeyCmdNewError:         "创建命令失败：%v",
	KeyCmdDeleteUsage:      "  /commands delete <名称>  - 删除命令",
	KeyCmdDeleteDone:       "命令 %q 已删除。",
	KeyCmdDeleteFail:       "删除命令 %q 失败：%v",
	KeyCmdShowUsage:        "  /commands show <名称>    - 查看命令内容",
	KeyCmdShowFail:         "命令 %q 未找到。",
	KeyCmdShowHeader:       "--- %s ---",
	KeyCronNotAvail:        "定时任务调度器不可用。",
	KeyNoCronTasks:         "没有定时任务。",
	KeyNoCronHint:          "使用 add_cron_task 工具创建定时任务。",
	KeyCronHeader:          "%-20s %-12s %s",
	KeyCronRecent:          "最近结果：",
	KeyResumePrompt:        "\n发现之前的会话 %s（%d 轮，%s 前）。恢复？[Y/n]：",
	KeyResumeYes:           "yes",
	KeyCancelAll:           "所有运行中的任务已取消。",
	KeyCancelTask:          "任务 %s 已取消。",
	KeyCancelNotFound:      "未找到运行中的任务，ID：%s",
	KeySessionCheckpoint:   "\n[会话检查点：摘要已保存，继续运行...]\n",
	KeyTurnError:           "\n[错误：%v]",
	KeyNoAvailableProvider: "→ 没有可用的 LLM 提供商 — 请检查配置\n  参考: https://gitee.com/dolphinzzv/dolphindolphin/blob/main/docs/zh/INSTALL.zh.md\n",

	// LLM 警告
	KeyWarnNoLLM:        "\n⚠  LLM 未配置 — 未找到 API 密钥。\n",
	KeyWarnDefaultModel: "   默认模型：%s（接口地址：%s）\n",
	KeyWarnSetAPIKey:    "   设置 DZ_LLM_API_KEY 环境变量或在配置中添加 api_key。\n",
	KeyWarnRunSetup:     "   运行：dolphin setup\n\n",

	// 提供商横幅
	KeyLLMProvidersHeader: "\nLLM 提供商：\n",
	KeyLLMProviderOK:      "  ✓ %s（%s）— %dms\n",
	KeyLLMProviderFail:    "  ✗ %s（%s）— %dms %s\n",
	KeyLLMUsing:           "→ 使用：%s\n",

	// /status 命令
	KeyStatusHeader:   "状态：",
	KeyStatusProvider: "  提供商：     %s",
	KeyStatusModel:    "  模型：       %s",
	KeyStatusSession:  "  会话：       %s（%d 轮）",
	KeyStatusAgents:   "  代理：       %d（%d 忙碌）",
	KeyStatusMCPTools: "  MCP 工具：   %d",
	KeyStatusSkills:   "  技能：       %d",
	KeyStatusCommands: "  命令：       %d",
	KeyStatusCron:     "  定时任务：   %d",
	KeyStatusMemory:   "  内存：       %d MB",
	KeyNoSession:      "  会话：       无",

	// /sessions 命令
	KeySessionsHeader: "会话（%d）：",
	KeyNoSessions:     "未找到历史会话。",
	KeySessionRow:     "  %s  %4d 轮  %s  入=%d 出=%d",

	// /context
	KeyHelpReload:        "  /reload       - 重新加载（重启）代理",
	KeyHelpConfig:        "  /config       - 列出所有配置  |  /config get <路径>  |  /config set <路径> <值>",
	KeyHelpContext:       "  /context       - 上下文摘要 (/context system /context current /context <章节>)",
	KeyContextSummaryHd:  "=== 上下文摘要 ===",
	KeyContextSectionNF:  "章节 %q 未找到。",
	KeyContextSectionHd:  "=== %s ===",
	KeyContextProvider:   "提供商：      %s（%s）",
	KeyContextConfigPath: "配置路径：    %d 个",
	KeyContextMCPTools:   "MCP 工具：    %d 个已注册",
	KeyContextAgents:     "代理：        %d（%d 忙碌）",
	KeyContextSkills:     "技能：        %d 个可用",
	KeyContextSkillsNA:   "技能：        不可用",
	KeyContextCommands:   "命令：        %d 个可用",
	KeyContextCommandsNA: "命令：        不可用",
	KeyContextCron:       "定时任务：    %d 个已调度",
	KeyContextSelfEvolve: "自进化：      %v",
	KeyContextSectionsHd: "--- 上下文章节（优先级 · 大小 · 路径）---",
	KeyWelcomeBanner:     "dolphin — AI Agent",

	// pprof
	KeyPprofBanner:   "\n=== pprof 服务监听在 %s ===\n",
	KeyPprofURL:      "  http://%s/debug/pprof/\n",
	KeyMetricsBanner: "\n=== Metrics 服务监听在 %s ===\n",
	KeyMetricsURL:    "  http://%s/metrics\n",

	// Cobra command descriptions
	KeyCmdDolphinUse:   "dolphin",
	KeyCmdDolphinShort: "AI Agent — 支持 stdio / SSH / MQTT / Email 传输，MCP 工具（shell + cdp）",
	KeyCmdDolphinLong: `dolphin 是一个支持 MCP 工具的 AI Agent。

传输层: stdio（默认）、SSH (:2222)、MQTT、Email
工具: shell、cdp（浏览器自动化）
配置: .dolphin/config.yaml > ~/.dolphin/ > /etc/dolphin/
环境变量: DZ_LLM_API_KEY, DZ_LLM_MODEL, DZ_LLM_BASE_URL`,

	KeyCmdCompletionUse:   "completion [bash|zsh|fish|powershell]",
	KeyCmdCompletionShort: "生成 shell 补全脚本",
	KeyCmdCompletionLong: `生成 dolphin 命令的 shell 补全脚本。

输出指定 shell 的补全脚本，source 后即可启用 Tab 补全。

  bash:       source <(dolphin completion bash)
  zsh:        source <(dolphin completion zsh)
  fish:       dolphin completion fish | source
  powershell: dolphin completion powershell | Out-String | Invoke-Expression

永久生效（bash）:
  dolphin completion bash > /etc/bash_completion.d/dolphin

永久生效（zsh）:
  dolphin completion zsh > "${fpath[1]}/_dolphin"`,

	KeyCmdConfigUse:       "config",
	KeyCmdConfigShort:     "管理配置",
	KeyCmdConfigShowUse:   "show",
	KeyCmdConfigShowShort: "显示当前生效的配置",

	KeyCmdDoctorUse:   "doctor",
	KeyCmdDoctorShort: "运行自诊断检查",
	KeyCmdDoctorLong: `在系统上运行自诊断检查，识别配置问题。

检查项目:
  - 配置文件位置与可解析性
  - LLM API 密钥存在性与端点连通性
  - 会话目录可访问性
  - 传输配置一致性
  - SSH 主机密钥可用性
  - Skills 和 MCP 目录
  - 已启用传输的端口可用性`,

	KeyCmdInitUse:   "init",
	KeyCmdInitShort: "生成默认配置文件",
	KeyCmdInitLong: `生成带注释的 .dolphin/config.yaml 默认配置。

使用 --restrictive 生成安全加固配置：
  - Shell 命令限制为安全白名单
  - CDP 浏览器自动化禁用
  - Webhook 工具禁用
  - 日志级别设为 warn
  - 插件禁用`,

	KeyCmdNewUse:   "new",
	KeyCmdNewShort: "从干净状态启动新的 dolphin 会话",
	KeyCmdNewLong: `清理所有 dolphin 运行时数据和状态，然后启动全新的
dolphin agent 会话。

移除内容:
  - 所有运行时数据（会话、日记、日志、工作区、定时任务）
  - SSH 自动生成的密码
  - 缓存的工具清单
  - 下载的技能和命令
  - SYSTEM.md（系统提示）
  - /etc/dolphin/ 系统级数据
  - 首次运行标记

配置文件（config.yaml）将被保留。`,

	KeyCmdResetUse:   "reset",
	KeyCmdResetShort: "重置 dolphin 到干净状态",
	KeyCmdResetLong: `删除所有运行时数据、自动生成的文件和首次运行标记，
下次启动时就像第一次使用一样。

移除的运行时数据:
  - 会话、日记、日志、工作区、定时任务
  - SSH 自动生成的密码
  - 缓存的工具清单
  - 下载的技能和命令
  - SYSTEM.md（系统提示）
  - /etc/dolphin/ 系统级配置和数据
  - 首次运行标记（下次启动时显示设置向导）
  - Email 已配置标记（下次 email 会话时重新发送启动邮件）

配置文件（config.yaml）将被保留。`,

	KeyCmdSessionsUse:       "sessions",
	KeyCmdSessionsShort:     "列出和管理 agent 会话",
	KeyCmdSessionsShowUse:   "show <id>",
	KeyCmdSessionsShowShort: "以可读对话形式显示会话详情",
	KeyCmdSessionsLogUse:    "log <id>",
	KeyCmdSessionsLogShort:  "显示原始会话事件日志",
	KeyCmdSessionsRmUse:     "rm <id>",
	KeyCmdSessionsRmShort:   "删除会话文件",
	KeyCmdSessionsDumpUse:   "dump <id>",
	KeyCmdSessionsDumpShort: "生成会话的 Mermaid 时序图",

	KeyCmdSkillsUse:            "skills",
	KeyCmdSkillsShort:          "列出和管理技能",
	KeyCmdSkillsListUse:        "list",
	KeyCmdSkillsListShort:      "列出所有已安装的技能",
	KeyCmdSkillsSearchUse:      "search <查询词>",
	KeyCmdSkillsSearchShort:    "按名称或描述搜索技能",
	KeyCmdSkillsInstallUse:     "install <名称> [描述]",
	KeyCmdSkillsInstallShort:   "从模板安装新技能",
	KeyCmdSkillsNewUse:         "new <名称> [描述]",
	KeyCmdSkillsNewShort:       "从模板创建新技能",
	KeyCmdSkillsDisableUse:     "disable <名称>",
	KeyCmdSkillsDisableShort:   "禁用并删除技能",
	KeyCmdSkillsEnableUse:      "enable <名称>",
	KeyCmdSkillsEnableShort:    "启用已禁用的技能",
	KeyCmdSkillsUninstallUse:   "uninstall <名称>",
	KeyCmdSkillsUninstallShort: "永久卸载技能",
	KeyCmdAgentUse:             "agent",
	KeyCmdAgentShort:           "列出和管理持久化 Agent",
	KeyCmdAgentListUse:         "list",
	KeyCmdAgentListShort:       "列出所有已安装的 Agent",
	KeyCmdAgentSearchUse:       "search <查询词>",
	KeyCmdAgentSearchShort:     "按名称或描述搜索 Agent",
	KeyCmdAgentInstallUse:      "install <名称>",
	KeyCmdAgentInstallShort:    "从仓库安装 Agent",
	KeyCmdAgentNewUse:          "new <名称>",
	KeyCmdAgentNewShort:        "从模板创建新 Agent",
	KeyCmdAgentDisableUse:      "disable <名称>",
	KeyCmdAgentDisableShort:    "禁用 Agent（保留文件）",
	KeyCmdAgentEnableUse:       "enable <名称>",
	KeyCmdAgentEnableShort:     "启用已禁用的 Agent",
	KeyCmdAgentUninstallUse:    "uninstall <名称>",
	KeyCmdAgentUninstallShort:  "永久卸载 Agent",
	KeyCmdMCPUse:               "mcp",
	KeyCmdMCPShort:             "列出和管理 MCP 服务器",
	KeyCmdMCPSearchUse:         "search <查询词>",
	KeyCmdMCPSearchShort:       "按名称或描述搜索 MCP 服务器",
	KeyCmdMCPInstallUse:        "install <名称>",
	KeyCmdMCPInstallShort:      "从仓库安装 MCP 服务器",
	KeyCmdMCPUninstallUse:      "uninstall <名称>",
	KeyCmdMCPUninstallShort:    "卸载 MCP 服务器",
	KeyCmdMCPEnableUse:         "enable <名称>",
	KeyCmdMCPEnableShort:       "启用 MCP 服务器",
	KeyCmdMCPDisableUse:        "disable <名称>",
	KeyCmdMCPDisableShort:      "禁用 MCP 服务器",

	KeyCmdStatusUse:   "status",
	KeyCmdStatusShort: "显示 dolphin 守护进程健康状态与配置",

	KeyCmdUpdateUse:   "update [版本号]",
	KeyCmdUpdateShort: "从 GitHub 更新 dolphin 到最新版本或指定版本",
	KeyCmdUpdateLong: `从 GitHub Releases 下载安装指定版本的 dolphin。

如不指定版本号，则使用最新版本。
版本号需对应 GitHub Release 标签（如 "v1.0.0"）。

示例:
  dolphin update          更新到最新版本
  dolphin update v1.0.0   更新到指定版本`,

	KeyCmdVersionUse:   "version",
	KeyCmdVersionShort: "打印版本号",

	KeyCmdConfigFlag: "配置文件路径（默认搜索 .dolphin/、~/.dolphin/、/etc/dolphin/）",

	// Root command flags
	KeyFlagConfig:  "配置文件路径（默认搜索 .dolphin/、~/.dolphin/、/etc/dolphin/）",
	KeyFlagVerbose: "启用 debug 级别日志",
	KeyFlagQuiet:   "抑制非错误输出",

	// Doctor output
	KeyDoctorBanner:              "Dolphin 诊断工具",
	KeyDoctorSep:                 "================",
	KeyDoctorOK:                  "  [OK]   %s: %s",
	KeyDoctorWarn:                "  [WARN] %s: %s",
	KeyDoctorFail:                "  [FAIL] %s: %s",
	KeyDoctorResults:             "结果: %d 通过, %d 警告, %d 失败",
	KeyDoctorFixHint:             "运行 'dolphin setup' 修复配置问题。",
	KeyDoctorCfgValid:            "配置已加载并验证",
	KeyDoctorCfgFail:             "config.Load 失败: %v",
	KeyDoctorLLMKeyOK:            "已配置",
	KeyDoctorLLMKeyFail:          "未找到 API 密钥 — 设置 DZ_LLM_API_KEY 环境变量或运行 'dolphin setup'",
	KeyDoctorLLMProvNone:         "%s 不可达: %v（检查网络或代理）",
	KeyDoctorLLMBaseEmpty:        "Base URL 为空",
	KeyDoctorLLMReachable:        "%s 可达",
	KeyDoctorLLMUnreachable:      "%s 不可达: %v（检查网络或代理）",
	KeyDoctorSessOK:              "会话目录可写",
	KeyDoctorSessNotExist:        "%s 不存在（首次运行时将自动创建）",
	KeyDoctorSessFail:            "%s: %v",
	KeyDoctorSessNotDir:          "%s 不是目录",
	KeyDoctorSessNotWritable:     "%s 不可写: %v",
	KeyDoctorTransStdio:          "已启用",
	KeyDoctorTransSSH:            "已启用于 %s（用户: %s）",
	KeyDoctorTransMQTT:           "已启用（Broker: %s）",
	KeyDoctorTransEmail:          "已启用（发件人: %s）",
	KeyDoctorTransNone:           "没有启用任何传输 — 至少启用一种（stdio、ssh、mqtt 或 email）",
	KeyDoctorSSHPassFail:         "SSH 密码为空 — 将自动生成，请检查日志",
	KeyDoctorSSHKeyFail:          "无法展开 ~: %v",
	KeyDoctorSSHKeyWarn:          "在 %s 或 %s 未找到主机密钥 — 将自动生成临时密钥",
	KeyDoctorSSHKeyOK:            "找到主机密钥",
	KeyDoctorSSHKeyAuto:          "自动生成的密钥位于 %s",
	KeyDoctorSkillsDirOK:         "skills 目录可访问",
	KeyDoctorSkillsDirWarn:       "%s 不存在（首次运行时将自动创建）",
	KeyDoctorSkillsDirFail:       "%s: %v",
	KeyDoctorSkillsDirNotDir:     "%s 不是目录",
	KeyDoctorShellDisabled:       "shell 工具已禁用（mcp.shell.enabled=false）",
	KeyDoctorShellUnrestricted:   "无限制模式 — 允许所有 shell 命令",
	KeyDoctorShellRestricted:     "限制为: %v",
	KeyDoctorShellDefault:        "已启用，使用默认限制",
	KeyDoctorPortAvail:           "%s 可用",
	KeyDoctorPortInUse:           "%s 占用或不可用（%v）",
	KeyDoctorCfgNotFound:         "未找到（%s），跳过",
	KeyDoctorCfgUnreadable:       "不可读: %v",
	KeyDoctorCheckNameCfgSys:     "系统配置",
	KeyDoctorCheckNameCfgUser:    "用户配置",
	KeyDoctorCheckNameCfgProj:    "项目配置",
	KeyDoctorCheckNameCfgVal:     "配置验证",
	KeyDoctorCheckNameLLMKey:     "LLM API 密钥",
	KeyDoctorCheckNameSessDir:    "会话目录",
	KeyDoctorCheckNameTransStdio: "传输 stdio",
	KeyDoctorCheckNameTransSSH:   "传输 ssh",
	KeyDoctorCheckNameTransMQTT:  "传输 mqtt",
	KeyDoctorCheckNameTransEmail: "传输 email",
	KeyDoctorCheckNameTransports: "传输",
	KeyDoctorCheckNameSSHPass:    "SSH 密码",
	KeyDoctorCheckNameSSHKey:     "SSH 主机密钥",
	KeyDoctorCheckNameSkillsDir:  "skills 目录",
	KeyDoctorCheckNameShell:      "MCP shell",
	KeyDoctorUnreadable:          "不可读: %v",
	KeyDoctorLLMProvFail:         "未配置提供商",

	// Status output
	KeyStatusVersion:           "版本: %s",
	KeyStatusBuild:             "构建: %s",
	KeyStatusLLM:               "LLM:       已配置",
	KeyStatusLLMNotCfg:         "LLM:       未配置（运行 'dolphin setup'）",
	KeyStatusHealthUnreach:     "健康检查: 不可达（%v）",
	KeyStatusHealthOK:          "健康检查: 正常 — %s",
	KeyStatusHealthDisabled:    "健康检查: 已禁用（设置 health.enabled=true）",
	KeyStatusMetricsEnabled:    "指标:     已启用于 %s",
	KeyStatusMetricsDisabled:   "指标:     已禁用",
	KeyStatusTransports:        "传输:",
	KeyStatusShell:             "Shell:",
	KeyStatusShellUnrestricted: "Shell:    无限制（允许管道和重定向）",
	KeyStatusShellRestricted:   "Shell:    受限（允许: %v）",
	KeyStatusTransStdio:        "  - stdio: 已启用",
	KeyStatusTransSSH:          "  - ssh:   已启用于 %s",
	KeyStatusTransMQTT:         "  - mqtt:  已启用（broker: %s）",
	KeyStatusTransEmail:        "  - email: 已启用（发件人: %s）",

	// Update output
	KeyUpdateCurrent:       "当前版本: %s",
	KeyUpdatePlatform:      "平台: %s/%s",
	KeyUpdateRelease:       "版本: %s",
	KeyUpdateAlreadyLatest: "已是最新版本 %s，无需更新。",
	KeyUpdateReady:         "\n准备下载并安装 %s（%s）",
	KeyUpdateBinary:        "当前二进制: %s",
	KeyUpdateConfirm:       "确定要更新吗？[y/N]: ",
	KeyUpdateCancelled:     "更新已取消。",
	KeyUpdateDownloading:   "\n正在下载 %s ...",
	KeyUpdateComplete:      "\n已更新到 %s",
	KeyUpdateVerify:        "运行 'dolphin --version' 验证。",
	KeyUpdateNoReleases:    "未找到发布版本。",
	KeyUpdateAvailable:     "可用版本（%s/%s）:",
	KeyUpdatePreRelease:    "\n⚠ = 预发布版本",

	// Setup output

	// Init output
	KeyInitRestrictiveGenerated: "\n安全加固配置已生成: %s",
	KeyInitRestrictiveDiffs:     "\n与默认配置的主要区别:",
	KeyInitRestrictiveShell:     "  - Shell: 仅允许白名单命令（ls、cat、grep、find ...）",
	KeyInitRestrictiveCDP:       "  - CDP 浏览器: 已禁用",
	KeyInitRestrictiveWebhook:   "  - Webhook: 已禁用",
	KeyInitRestrictiveLog:       "  - 日志级别: warn",
	KeyInitRestrictivePlugins:   "  - 插件: 已禁用",
	KeyInitRestrictiveRun:       "\n运行 'dolphin' 使用此配置启动。",
	KeyInitDefaultGenerated:     "默认配置文件已生成: %s",
	KeyInitEditAndRun:           "编辑后运行 'dolphin' 启动。",
	KeyInitGitError:             "git init .dolphin: %v",
	KeyInitRestrictiveFlag:      "生成安全加固配置（限制 shell、禁用 CDP/webhook、warn 日志级别）",

	// Reset output
	KeyResetWillRemove:  "以下内容将被移除:",
	KeyResetComplete:    "重置完成: %d 项已移除",
	KeyResetMarkerReset: "首次运行标记已重置。",
	KeyResetRunAgain:    "运行 'dolphin' 重新进入初始设置向导。",
	KeyResetConfirm:     "\n确定要执行吗？此操作无法撤销。[y/N]: ",
	KeyResetCancelled:   "%s 已取消。",

	// New session output
	KeyNewStarting: "正在启动全新的 dolphin 会话:",

	// Sessions output
	KeySessNoDir:        "未找到会话（目录不存在）",
	KeySessNone:         "未找到会话。",
	KeySessDirLabel:     "会话目录: %s",
	KeySessNotFound:     "会话 %q 未找到",
	KeySessNoEvents:     "会话中没有事件。",
	KeySessHeader:       "会话: %s",
	KeySessDuration:     "持续时间: %s — %s（%d 个事件）",
	KeySessTurnTokens:   "（Token: %d 入 / %d 出）",
	KeySessRemoved:      "已删除会话 %q",
	KeySessDumpNoEvents: "会话中没有事件",
	KeySessServing:      "服务地址: %s",
	KeySessStopHint:     "按 Ctrl+C 停止。",

	// Config show output
	KeyCfgShowLLM:            "LLM:",
	KeyCfgShowSession:        "会话:",
	KeyCfgShowTransports:     "传输:",
	KeyCfgShowMCP:            "MCP 工具:",
	KeyCfgShowAgentPool:      "Agent 池:",
	KeyCfgShowSkills:         "技能:",
	KeyCfgShowCrontab:        "定时任务:",
	KeyCfgShowMonitoring:     "监控:",
	KeyCfgShowPlugins:        "插件:",
	KeyCfgShowLogLevel:       "日志级别: %s",
	KeyCfgShowLogFile:        "日志文件: %s",
	KeyCfgShowEnabled:        "已启用",
	KeyCfgShowDisabled:       "已禁用",
	KeyCfgShowType:           "  类型:       %s",
	KeyCfgShowModel:          "  模型:       %s",
	KeyCfgShowBaseURL:        "  Base URL:   %s",
	KeyCfgShowAPIKey:         "  API Key:    %s",
	KeyCfgShowMaxTokens:      "  最大 Token:  %d",
	KeyCfgShowMaxCtxTokens:   "  最大上下文:  %d",
	KeyCfgShowTemperature:    "  温度:       %.1f",
	KeyCfgShowMaxSubTurns:    "  最大子轮次: %d",
	KeyCfgShowCompressMode:   "  压缩模式:   %s",
	KeyCfgShowMaxLoop:        "  最大循环:   %d",
	KeyCfgShowSummary:        "  摘要:       %v",
	KeyCfgShowMaxAge:         "  最大保留:   %s",
	KeyCfgShowShell:          "  Shell:   enabled=%v",
	KeyCfgShowCDP:            "  CDP:     enabled=%v",
	KeyCfgShowEmail:          "  Email:   enabled=%v",
	KeyCfgShowRepos:          "  仓库:       %v",
	KeyCfgShowMaxConcurrency: "  最大并发:      %d",
	KeyCfgShowDefaultTimeout: "  默认超时:      %ds",
	KeyCfgShowIdleTimeout:    "  空闲超时:      %ds",
	KeyCfgShowWorkspace:      "  工作区:        %s",
	KeyCfgShowMaxPending:     "  最大待处理结果: %d",
	KeyCfgShowDir:            "  目录:     %s",
	KeyCfgShowMaxTop:         "  最大 Top: %d",
	KeyCfgShowFile:           "  文件:            %s",
	KeyCfgShowCheckInterval:  "  检查间隔:  %s",
	KeyCfgShowRestricted:     "（受限: %v）",
	KeyCfgShowUnrestricted:   "（不受限）",
	KeyCfgShowDefault:        "（默认）",
	KeyCfgShowRemote:         "（远程: %s）",
	KeyCfgShowHeadless:       "（无头: %v）",
	KeyCfgShowServer:         "  Server(%s): %s（类型: %s）",

	// Transport startup messages
	KeyTransSSHServer:   "\n=== SSH 服务已配置于 %s ===\n",
	KeyTransSSHConnect:  "连接方式: ssh %s@<host> -p %s",
	KeyTransMQTTActive:  "\n=== MQTT 传输已激活 ===\n",
	KeyTransMQTTBroker:  "Broker: %s  Topic: %s  Client: %s",
	KeyTransEmailActive: "\n=== Email 传输已激活 ===\n",
	KeyTransEmailIMAP:   "IMAP: %s:%d（轮询间隔 %s）",
	KeyTransEmailSMTP:   "SMTP: %s:%d",
	KeyTransEmailHint:   "发送邮件到 %s — 主题 = 命令",
	KeyTransDingTalk:    "\n=== DingTalk 机器人已激活（Stream 模式）===\n",
	KeyTransACPActive:   "\n=== ACP 传输已激活（IBM BeeAI ACP）于 %s ===\n",
	KeyTransA2AActive:   "\n=== A2A 传输已激活（Google Agent-to-Agent）于 %s ===\n",
	KeyTransNoneEnabled: "没有启用任何传输（请在配置中启用 stdio、ssh、mqtt 或 email）",

	// Common
	KeyEnabled:    "已启用",
	KeyDisabled:   "已禁用",
	KeyNotFound:   "未找到",
	KeyError:      "错误",
	KeySkipped:    "已跳过",
	KeyCancelled:  "已取消",
	KeyAreYouSure: "确定要执行吗？[y/N]: ",
	KeyYes:        "是",
	KeyNo:         "否",

	// Skills CLI output
	KeySkillsCLINone:        "未安装任何技能。",
	KeySkillsCLITotal:       "\n总计: %d 个技能",
	KeySkillsCLIInstalled:   "技能 %q 已安装到 %s",
	KeySkillsCLISearchNone:  "未找到匹配 %q 的技能。",
	KeySkillsCLIFound:       "\n找到 %d 个匹配 %q 的结果（* = 已安装）。",
	KeySkillsCLIEdit:        "编辑该文件以添加技能内容。",
	KeySkillsCLICreated:     "技能 %q 已创建到 %s",
	KeySkillsCLIDisabled:    "技能 %q 已禁用并删除。",
	KeySkillsCLIEnabled:     "技能 %q 已启用。",
	KeySkillsCLIUninstalled: "技能 %q 已卸载。",
	KeyMCPCLINone:           "未安装任何 MCP 服务器。",
	KeyMCPCLITotal:          "\n总计: %d 个 MCP 服务器",
	KeyMCPCLIInstalled:      "MCP 服务器 %q 已安装。",
	KeyMCPCLISearchNone:     "未找到匹配 %q 的 MCP 服务器。",
	KeyMCPCLIFound:          "\n找到 %d 个匹配 %q 的结果。",
	KeyMCPCLIUninstalled:    "MCP 服务器 %q 已卸载。",
	KeyMCPCLIEnabled:        "MCP 服务器 %q 已启用。",
	KeyMCPCLIDisabled:       "MCP 服务器 %q 已禁用。",
	KeyAgentCLINone:         "未安装任何 Agent。",
	KeyAgentCLITotal:        "\n总计: %d 个 Agent",
	KeyAgentCLIInstalled:    "Agent %q 安装成功。",
	KeyAgentCLICreated:      "Agent %q 已创建到 %s",
	KeyAgentCLISearchNone:   "未找到匹配 %q 的 Agent。",
	KeyAgentCLIFound:        "\n找到 %d 个匹配 %q 的结果（* = 已安装）。",
	KeyAgentCLIDisabled:     "Agent %q 已禁用。",
	KeyAgentCLIEnabled:      "Agent %q 已启用。",
	KeyAgentCLIUninstalled:  "Agent %q 已永久卸载。",

	// Cleanup common
	KeyCleanupComplete: "清理完成: %d 项已移除",
	KeyNotExistSkip:    "（未找到，跳过）",
	KeyDirectory:       "/（目录）",
}
