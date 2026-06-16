package models

import "dolphin/internal/llm"

func init() {
	Match("deepseek-v4-pro", "*", func(req *llm.LLMRequest) {
		if req.ReasoningEffort == "" {
			req.ReasoningEffort = "high"
		}
	})
}
