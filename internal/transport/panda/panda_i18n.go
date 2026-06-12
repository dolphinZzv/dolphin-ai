package panda

import "dolphin/internal/i18n"

func init() {
	i18n.Register("panda",
		"en", i18n.Dict{
			"denied":         "Bot whitelist is not configured. Please contact the administrator to add %s.",
			"no_interactive": "panda-ai transport does not support interactive permission requests. Add rules to permissions.json.",
		},
		"zh", i18n.Dict{
			"denied":         "机器人暂未配置白名单，请联系管理员配置添加 %s 后使用",
			"no_interactive": "panda-ai 传输不支持交互式权限请求，请在 permissions.json 中添加规则",
		},
	)
}
