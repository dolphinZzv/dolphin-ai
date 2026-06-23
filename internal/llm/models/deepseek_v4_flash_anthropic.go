package models

import "dolphin/internal/llm"

func init() {
	llm.RegisterModelProvider("deepseek-v4-flash/anthropic", NewAnthropicProvider("deepseek-v4-flash"))
}
