package agentio

import "dolphin/internal/i18n"

func init() {
	i18n.Register("agentio",
		"en", i18n.Dict{
			"reply_prefix":           "\n%s> ",
			"session_switched":       "--- Session switched to: %s ---",
			"session_broadcast":      "\n--- Session switched to: %s ---\n",
			"session_expired_prompt": "Session has been idle for %s. Start a new one? (y = new, n = continue current)",
		},
		"zh", i18n.Dict{
			"reply_prefix":           "\n%s> ",
			"session_switched":       "--- 会话已切换到: %s ---",
			"session_broadcast":      "\n--- 会话已切换到: %s ---\n",
			"session_expired_prompt": "会话已闲置 %s，是否开启新会话？(y=新开 / n=继续当前会话)",
		},
	)
}
