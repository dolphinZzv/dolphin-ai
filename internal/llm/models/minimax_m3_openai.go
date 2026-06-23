package models

import "dolphin/internal/llm"

func init() {
	llm.RegisterModelProvider("minimax-m3/openai", NewOpenAIProvider("minimax-m3"))
}
