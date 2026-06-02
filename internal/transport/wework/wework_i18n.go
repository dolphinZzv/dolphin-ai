package wework

import "dolphin/internal/i18n"

func init() {
	i18n.Register("wework",
		"en", i18n.Dict{
			"context":        "Current message is from WeWork. Use the FILE_UPLOAD tool to upload and send files/images, or the MESSAGE tool to send text/markdown messages proactively. WeWork markdown does not support inline images. After uploading, images will be sent as native image messages.",
			"denied":         "Bot whitelist is not configured. Please contact the administrator to add %s.",
			"no_interactive": "WeWork transport does not support interactive permission requests. Add rules to permissions.json.",
		},
		"zh", i18n.Dict{
			"context":        "当前消息来自企业微信。请使用 FILE_UPLOAD 工具上传和发送文件/图片，或使用 MESSAGE 工具主动发送文本/markdown 消息。企业微信 markdown 不支持内嵌图片，上传图片后工具会自动以原生图片消息发送到会话。",
			"denied":         "机器人暂未配置白名单，请联系管理员配置添加 %s 后使用",
			"no_interactive": "企业微信传输不支持交互式权限请求，请在 permissions.json 中添加规则",
		},
	)
}
