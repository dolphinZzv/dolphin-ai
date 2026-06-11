package config

import "dolphin/internal/i18n"

func init() {
	i18n.Register("config",
		"en", i18n.Dict{
			"missing_use":  "config: missing llm.use",
			"read_failed":  "config: read %s: %w",
			"parse_failed": "config: parse %s: %w",
		},
		"zh", i18n.Dict{
			"missing_use":  "配置: 缺少 llm.use",
			"read_failed":  "配置: 读取 %s 失败: %w",
			"parse_failed": "配置: 解析 %s 失败: %w",
		},
	)
}
