package llm

import (
	"context"
	"time"

	"dolphin/internal/types"
)

type ModelConfig struct {
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	Vendor      string   `json:"vendor,omitempty"`
	APIType     string   `json:"api_type,omitempty"`
	Model       string   `json:"model"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float64  `json:"temperature"`
	TopP        float64  `json:"top_p"`
	Stop        []string `json:"stop"`
}

type LLMRequest struct {
	Messages  []types.Message
	System    string
	Model     string
	MaxTokens int
	TopP      float64
	Stop      []string
	Tools     []types.ToolDef
}

type LLMChunk struct {
	Content           string
	Thinking          string
	ThinkingSignature string
	ToolCalls         []types.ToolCall
	Done              bool
	Error             error
	InputTokens       int
	OutputTokens      int

	// Cache statistics:
	//   Anthropic: cache_creation_input_tokens from message_start usage
	//   OpenAI:    prompt_tokens_details.cached_tokens from the final usage chunk
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	PromptCachedTokens       int
	TotalTokens              int
}

type Provider interface {
	Name() string
	CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
	Models(ctx context.Context) ([]ModelConfig, error)
}

type Config struct {
	Provider    string // display name (config section name)
	Vendor      string // vendor name for factory lookup (e.g. "deepseek")
	APIType     string // API protocol: "openai" or "anthropic" (defaults to Provider)
	Model       string
	APIKey      string
	BaseURL     string
	Temperature float64
	MaxTokens   int
	MaxRetries  int
	Timeout     time.Duration
	Headers     map[string]string // custom HTTP headers for this provider
	Models      []ModelConfig     // models registered for this provider
}
