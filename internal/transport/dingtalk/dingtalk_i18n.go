package dingtalk

import "dolphin/internal/i18n"

func init() {
	i18n.Register("dingtalk",
		"en", i18n.Dict{
			"context":              "Current message is from DingTalk group chat.",
			"denied":               "@%s Sorry, you do not have permission to use this bot.",
			"no_whitelist":         "Bot whitelist is not configured. Please contact the administrator.",
			"no_interactive":       "DingTalk transport does not support interactive permission requests. Add rules to permissions.json.",
			"startup_notification": "Dolphin AI assistant online ✓",
		},
		"zh", i18n.Dict{
			"context":              "当前消息来自钉钉群",
			"denied":               "@%s 抱歉，您没有权限使用此机器人",
			"no_whitelist":         "机器人暂未配置白名单，请联系管理员配置后使用",
			"no_interactive":       "钉钉传输不支持交互式权限请求，请在 permissions.json 中添加规则",
			"startup_notification": "Dolphin AI 助手已上线 ✓",
		},
	)
}
