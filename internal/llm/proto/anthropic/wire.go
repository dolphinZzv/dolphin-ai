package anthropic

import "dolphin/internal/llm"

// Wire the Anthropic discoverer into the llm package without creating an
// import cycle (proto/anthropic imports llm; llm cannot import proto/anthropic).
func init() {
	llm.SetAnthropicDiscoverer(DiscoverModels)
}
