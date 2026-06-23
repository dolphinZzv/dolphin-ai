package openai

import "dolphin/internal/llm"

// Wire the OpenAI discoverer into the llm package without creating an import
// cycle (proto/openai imports llm; llm cannot import proto/openai).
func init() {
	llm.SetOpenAIDiscoverer(DiscoverModels)
}
