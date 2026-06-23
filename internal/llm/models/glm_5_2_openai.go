package models

import "dolphin/internal/llm"

func init() {
	llm.RegisterModelProvider("glm-5.2/openai", NewOpenAIProvider("glm-5.2"))
}
