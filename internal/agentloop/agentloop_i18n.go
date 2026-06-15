package agentloop

import "dolphin/internal/i18n"

func init() {
	i18n.Register("agentloop",
		"en", i18n.Dict{
			"error_prefix":             "Error: ",
			"permission_denied":        "permission denied",
			"tool_denied":              "tool %q is denied by permission rules",
			"tool_requires_permission": "tool %q requires permission — add an allow rule to permissions.json",
			"tool_permission_request":  "Tool %q wants to execute.\nArguments: %s",
			"tool_permission_failed":   "tool %q permission request failed: %w",
			"tool_denied_by_user":      "tool %q was denied by the user",
			"denied_message":           "❌ %s",
			"tool_interrupted":         "tool %q was interrupted before completion",
			"stage_init_failed":        "init stage %s: %w",
			"stage_loop_failed":        "loop stage %s: %w",
		},
		"zh", i18n.Dict{
			"error_prefix":             "错误: ",
			"permission_denied":        "权限被拒绝",
			"tool_denied":              "工具 %q 已被权限规则拒绝",
			"tool_requires_permission": "工具 %q 需要权限 — 请在 permissions.json 中添加允许规则",
			"tool_permission_request":  "工具 %q 想要执行。\n参数: %s",
			"tool_permission_failed":   "工具 %q 权限请求失败: %w",
			"tool_denied_by_user":      "工具 %q 已被用户拒绝",
			"denied_message":           "❌ %s",
			"tool_interrupted":         "tool %q was interrupted before completion",
			"stage_init_failed":        "初始化阶段 %s 失败: %w",
			"stage_loop_failed":        "循环阶段 %s 失败: %w",
		},
	)
}
