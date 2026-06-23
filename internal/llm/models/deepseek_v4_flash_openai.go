package models

import "dolphin/internal/llm"

func init() {
	llm.RegisterModelProvider("deepseek-v4-flash/openai", NewOpenAIProvider("deepseek-v4-flash"))
}
