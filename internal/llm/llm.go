package llm

import (
	"context"
	"time"

	"dolphin/internal/types"
)

type ModelConfig struct {
	Name            string        `json:"name"`
	Provider        string        `json:"provider"`
	Vendor          string        `json:"vendor,omitempty"`
	APIType         string        `json:"api_type,omitempty"`
	Model           string        `json:"model"`
	MaxTokens       int           `json:"max_tokens"`
	Temperature     float64       `json:"temperature"`
	MaxRetries      int           `json:"max_retries"`
	Timeout         time.Duration `json:"timeout"`
	MaxConcurrency  int           `json:"max_concurrency,omitempty"`
	TopP            float64       `json:"top_p"`
	Stop            []string      `json:"stop"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	Thinking        bool          `json:"thinking,omitempty"`
	Stream          bool          `json:"stream"`
	StreamSet       bool          `json:"-"`
	Disabled        bool          `json:"disabled,omitempty"`
	// Headers are model-level custom HTTP headers. These override any
	// same-named headers from the section-level Config.Headers; other
	// section headers still apply. Used for per-model quirks (e.g. a
	// specific model requiring an X-Model-Version or route hint).
	Headers map[string]string `json:"headers,omitempty"`
}

type LLMRequest struct {
	Messages        []types.Message
	System          string
	Model           string
	MaxTokens       int
	Temperature     float64
	Timeout         time.Duration
	TopP            float64
	Stop            []string
	Tools           []types.ToolDef
	ReasoningEffort string
	Thinking        bool
	Stream          bool
	// StreamSet is true when Stream was explicitly set (e.g. from ModelConfig),
	// distinguishing "stream=false" from "stream not specified". Lets a provider
	// tell whether the caller chose streaming deliberately.
	StreamSet bool `json:"-"`
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
	//   DeepSeek:  prompt_cache_hit_tokens / prompt_cache_miss_tokens from usage
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	PromptCachedTokens       int
	PromptCacheHitTokens     int
	PromptCacheMissTokens    int
	TotalTokens              int
}

type Provider interface {
	Name() string
	CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
	Models(ctx context.Context) ([]ModelConfig, error)
}

// ProviderFunc adapts a CompleteStream function into a Provider. It is the
// zero-ceremony way for a per-model provider file to satisfy the interface
// without declaring a struct. Name/Models come from the supplied metadata.
type ProviderFunc struct {
	Name_   string
	Model_  ModelConfig
	Stream_ func(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error)
}

func (p ProviderFunc) Name() string { return p.Name_ }
func (p ProviderFunc) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	return p.Stream_(ctx, req)
}
func (p ProviderFunc) Models(ctx context.Context) ([]ModelConfig, error) {
	return []ModelConfig{p.Model_}, nil
}

type Config struct {
	Provider      string // display name (config section name)
	Vendor        string // vendor name for factory lookup (e.g. "deepseek")
	APIType       string // API protocol: "openai" or "anthropic" (defaults to Provider)
	Model         string
	APIKey        string
	BaseURL       string
	Temperature   float64
	MaxTokens     int
	MaxRetries    int
	Timeout       time.Duration
	Headers       map[string]string // custom HTTP headers for this provider
	Models        []ModelConfig     // models registered for this provider
	ModelDiscover bool              // fetch model list from vendor API
}
