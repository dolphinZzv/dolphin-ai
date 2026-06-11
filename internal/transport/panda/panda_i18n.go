package panda

import "dolphin/internal/i18n"

func init() {
	i18n.Register("panda",
		"en", i18n.Dict{
			"context": "Current message is from panda-ai IM. " +
				"When you have a screenshot or image, use SEND_IMAGE tool to upload and send it to the conversation immediately — never return a local file path to the user. " +
				"Use FILE_UPLOAD to upload files and get a URL, then include the returned markdown image in your reply. " +
				"Use MESSAGE tool to send text/markdown messages proactively.",
			"denied":         "Bot whitelist is not configured. Please contact the administrator to add %s.",
			"no_interactive": "panda-ai transport does not support interactive permission requests. Add rules to permissions.json.",
		},
		"zh", i18n.Dict{
			"context": "当前消息来自 panda-ai IM。 " +
				"当你有截图或图片时，请使用 SEND_IMAGE 工具上传并发送到会话中——绝对不要将本地文件路径返回给用户。 " +
				"使用 FILE_UPLOAD 上传文件获取 URL，然后在回复中使用返回的 markdown 图片语法嵌入图片。 " +
				"使用 MESSAGE 工具主动发送文本/markdown 消息。",
			"denied":         "机器人暂未配置白名单，请联系管理员配置添加 %s 后使用",
			"no_interactive": "panda-ai 传输不支持交互式权限请求，请在 permissions.json 中添加规则",
		},
	)
}
