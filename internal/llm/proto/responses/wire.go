package responses

import "dolphin/internal/llm"

func init() {
	llm.SetResponsesDiscoverer(DiscoverModels)
}
