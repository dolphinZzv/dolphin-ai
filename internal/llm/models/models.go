package models

import (
	"dolphin/internal/hook"
	"dolphin/internal/llm"
)

// Match registers an LLM hook that fires when model and apiType both match.
// Either may be "*" to match any value.
func Match(model, apiType string, fn func(req *llm.LLMRequest)) {
	hook.RegisterLLMRequestHook(func(req any, m, a string) {
		if (model == "*" || model == m) && (apiType == "*" || apiType == a) {
			if r, ok := req.(*llm.LLMRequest); ok {
				fn(r)
			}
		}
	})
}
