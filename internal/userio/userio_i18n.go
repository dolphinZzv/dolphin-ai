package userio

import "dolphin/internal/i18n"

func init() {
	i18n.Register("userio",
		"en", i18n.Dict{
			"script_not_found": "user executed /%s but no matching script was found, please analyze the user's intent and help",
		},
		"zh", i18n.Dict{
			"script_not_found": "用户执行了 /%s 但没有找到对应的脚本，请分析用户意图并提供帮助",
		},
	)
}
