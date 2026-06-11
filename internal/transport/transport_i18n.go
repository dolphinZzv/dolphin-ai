package transport

import "dolphin/internal/i18n"

func init() {
	i18n.Register("transport",
		"en", i18n.Dict{
			// general
			"unknown_type": "unknown transport type: %s",

			// stdio
			"stdio_exit_confirm":    "Confirm exit? (y/N) ",
			"stdio_bye":             "bye",
			"stdio_permission_menu": "\n1) Allow once  2) Always allow  3) Deny\nChoice (1/2/3): ",

			// email
			"email_send_only":      "email: send-only mode, cannot receive",
			"email_closed":         "email: closed",
			"email_subject":        "Subject: ",
			"email_reply_prefix":   "Re: ",
			"email_no_reply_to":    "email: no sender to reply to",
			"email_no_interactive": "email transport does not support interactive permission requests, add rules to permissions.json",
			"email_denied":         "Sorry, you are not authorized to send messages to this email address. Your message has been ignored.",
			"email_no_whitelist":   "Bot whitelist is not configured. Please contact the administrator.",
			"context_email":        "Current message is from email.",

			// panda
			"context_panda":        "Current message is from Panda AI IM server. Supports markdown responses.",
			"panda_no_interactive": "Panda transport does not support interactive permission requests. Add rules to permissions.json.",

			// permission
			"perm_no_interactive": "transport does not support interactive permission requests, add rules to permissions.json",
		},
		"zh", i18n.Dict{
			// general
			"unknown_type": "未知传输类型: %s",

			// stdio
			"stdio_exit_confirm":    "确认退出？(y/N) ",
			"stdio_bye":             "bye",
			"stdio_permission_menu": "\n1) 同意一次  2) 以后都同意  3) 拒绝\n选择 (1/2/3): ",

			// email
			"email_send_only":      "邮箱: 仅发送模式，无法接收",
			"email_closed":         "邮箱: 已关闭",
			"email_subject":        "标题: ",
			"email_reply_prefix":   "Re: ",
			"email_no_reply_to":    "邮箱: 没有可回复的发件人",
			"email_no_interactive": "邮箱传输不支持交互式权限请求，请在 permissions.json 中添加规则",
			"email_denied":         "抱歉，您没有权限向此邮箱发送消息，您的邮件已被忽略。",
			"email_no_whitelist":   "机器人暂未配置白名单，请联系管理员配置后使用",
			"context_email":        "当前消息来自邮件",

			// panda
			"context_panda":        "当前消息来自 Panda AI 即时通讯服务器。支持 markdown 格式回复。",
			"panda_no_interactive": "Panda 传输不支持交互式权限请求，请在 permissions.json 中添加规则",

			// permission
			"perm_no_interactive": "该传输方式不支持交互式权限请求，请在 permissions.json 中添加规则",
		},
	)
}
