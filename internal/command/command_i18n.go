package command

import "dolphin/internal/i18n"

func init() {
	i18n.Register("command",
		"en", i18n.Dict{
			// models
			"models_desc":      "List and switch LLM models",
			"models_list":      "List all available models",
			"models_switch":    "Switch to a model",
			"models_title":     "Available models:",
			"models_none":      "No models available",
			"models_name":      "Name",
			"models_vendor":    "Vendor",
			"models_api_type":  "API Type",
			"models_model":     "Model",
			"models_active":    " (active)",
			"models_total":     "  (total: %d models)\n",
			"models_no_switch": "switching models is not supported with the current provider\n",
			"models_switched":  "switched to %s\n",
			"models_error":     "error: %v\n",

			// session
			"session_manage":  "Manage sessions",
			"session_create":  "Create a new session",
			"session_created": "created session %s\n",
			"session_list":    "List all sessions",
			"session_none":    "no sessions",
			"session_active":  " [active]",
			"session_switch":  "Switch to a session (deprecated: use /session new)",
			"session_use_new": "use /session new to create and switch to a new session",

			// skills
			"skills_manage":       "List and manage skills",
			"skills_list":         "List all skills",
			"skills_list_error":   "list error: %v\n",
			"skills_none":         "No skills available",
			"skills_available":    "Available skills:",
			"skills_disabled":     "disabled",
			"skills_enabled":      "enabled",
			"skills_total":        "  (total: %d skills)\n",
			"skills_enable_cmd":   "Enable a skill",
			"skills_not_found":    "skill %q not found\n",
			"skills_enabled_msg":  "skill %q enabled\n",
			"skills_disable_cmd":  "Disable a skill",
			"skills_disabled_msg": "skill %q disabled\n",

			// version
			"version_desc":   "Print the version number",
			"version_output": "dolphin %s",

			// mcp tools
			"mcp_list_desc": "List loaded MCP tools",
			"mcp_none":      "No MCP tools loaded",
			"mcp_loaded":    "Loaded tools:",

			// scheduler
			"scheduler_list_desc": "List scheduled tasks",
			"scheduler_none":      "No scheduled tasks",
			"scheduler_tasks":     "Scheduled tasks:",
			"scheduler_cron":      "cron",
			"scheduler_delay":     "delay",
			"scheduler_pending":   "pending",
			"scheduler_disabled":  "disabled",

			// context
			"context_desc": "Show full system context (brain index, skills, etc.)",

			// commands
			"commands_manage":    "List and manage commands",
			"commands_list":      "List all commands",
			"commands_show":      "Show a command's details and content",
			"commands_none":      "No commands available",
			"commands_available": "Available commands:",
			"commands_enabled":   "enabled",
			"commands_disabled":  "disabled",
			"commands_total":     "  (total: %d commands)\n",
			"commands_not_found": "command %q not found\n",

			// error
			"error_format": "error: %v\n",

			// lang
			"lang_desc":      "List and switch languages",
			"lang_list":      "List all available languages",
			"lang_available": "Available languages:",
			"lang_active":    "(active)",
			"lang_use":       "Switch to a language",
			"lang_switched":  "switched to %s\n",
			"lang_invalid":   "invalid language: %s\n",
		},
		"zh", i18n.Dict{
			// models
			"models_desc":      "列出和切换模型",
			"models_list":      "列出所有可用模型",
			"models_switch":    "切换模型",
			"models_title":     "可用模型:",
			"models_none":      "没有可用模型",
			"models_name":      "名称",
			"models_vendor":    "供应商",
			"models_api_type":  "API 类型",
			"models_model":     "模型",
			"models_active":    " (当前)",
			"models_total":     "  (共 %d 个模型)\n",
			"models_no_switch": "当前供应商不支持切换模型\n",
			"models_switched":  "已切换到 %s\n",
			"models_error":     "错误: %v\n",

			// session
			"session_manage":  "管理会话",
			"session_create":  "创建新会话",
			"session_created": "已创建会话 %s\n",
			"session_list":    "列出所有会话",
			"session_none":    "没有会话",
			"session_active":  " [当前]",
			"session_switch":  "切换会话（已弃用，请使用 /session new）",
			"session_use_new": "请使用 /session new 创建并切换到新会话",

			// skills
			"skills_manage":       "管理和查看技能",
			"skills_list":         "列出所有技能",
			"skills_list_error":   "列表错误: %v\n",
			"skills_none":         "没有可用技能",
			"skills_available":    "可用技能:",
			"skills_disabled":     "已禁用",
			"skills_enabled":      "已启用",
			"skills_total":        "  (共 %d 个技能)\n",
			"skills_enable_cmd":   "启用一个技能",
			"skills_not_found":    "技能 %q 未找到\n",
			"skills_enabled_msg":  "技能 %q 已启用\n",
			"skills_disable_cmd":  "禁用一个技能",
			"skills_disabled_msg": "技能 %q 已禁用\n",

			// version
			"version_desc":   "打印版本号",
			"version_output": "dolphin %s",

			// mcp tools
			"mcp_list_desc": "列出已加载的 MCP 工具",
			"mcp_none":      "没有已加载的 MCP 工具",
			"mcp_loaded":    "已加载的工具:",

			// scheduler
			"scheduler_list_desc": "列出定时任务",
			"scheduler_none":      "没有定时任务",
			"scheduler_tasks":     "定时任务:",
			"scheduler_cron":      "周期",
			"scheduler_delay":     "延迟",
			"scheduler_pending":   "等待中",
			"scheduler_disabled":  "已禁用",

			// context
			"context_desc": "显示完整系统上下文（大脑索引、技能等）",

			// commands
			"commands_manage":    "列出和管理命令",
			"commands_list":      "列出所有命令",
			"commands_show":      "查看命令详情",
			"commands_none":      "没有可用命令",
			"commands_available": "可用命令:",
			"commands_enabled":   "已启用",
			"commands_disabled":  "已禁用",
			"commands_total":     "  (共 %d 个命令)\n",
			"commands_not_found": "命令 %q 未找到\n",

			// error
			"error_format": "错误: %v\n",

			// lang
			"lang_desc":      "列出和切换语言",
			"lang_list":      "列出所有可用语言",
			"lang_available": "可用语言:",
			"lang_active":    "（当前）",
			"lang_use":       "切换语言",
			"lang_switched":  "已切换到 %s\n",
			"lang_invalid":   "无效语言: %s\n",
		},
	)
}
