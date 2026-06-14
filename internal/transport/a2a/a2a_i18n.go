package a2a

import "dolphin/internal/i18n"

func init() {
	i18n.Register("a2a",
		"en", i18n.Dict{
			"context":        "Current message is from an A2A (Agent-to-Agent) protocol client.",
			"no_interactive": "A2A transport does not support interactive permission requests. Add rules to permissions.json.",
		},
		"zh", i18n.Dict{
			"context":        "当前消息来自 A2A (Agent-to-Agent) 协议客户端。",
			"no_interactive": "A2A 传输不支持交互式权限请求，请在 permissions.json 中添加规则。",
		},
	)
}
