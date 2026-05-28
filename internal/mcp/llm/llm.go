package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/agent/provider"
	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"go.uber.org/zap"
)

type Tool struct {
	providers    []config.ProviderConfig
	defaultProv  string
	defaultModel string
}

func New(cfg *config.Config) *Tool {
	providers := cfg.LLM.EffectiveProviders()

	var defaultProv, defaultModel string
	if len(providers) > 0 {
		defaultProv = providers[0].Name
		defaultModel = providers[0].Model
	}

	return &Tool{
		providers:    providers,
		defaultProv:  defaultProv,
		defaultModel: defaultModel,
	}
}

// OnConfigChange rebuilds the provider snapshot from the new config.
func (t *Tool) OnConfigChange(oldCfg, newCfg *config.Config) {
	providers := newCfg.LLM.EffectiveProviders()
	t.providers = providers
	if len(providers) > 0 {
		t.defaultProv = providers[0].Name
		t.defaultModel = providers[0].Model
	} else {
		t.defaultProv = ""
		t.defaultModel = ""
	}
}

func (t *Tool) Definition() mcp.ToolDefinition {
	providerEnum := make([]string, 0, len(t.providers))
	for _, p := range t.providers {
		providerEnum = append(providerEnum, p.Name)
	}

	modelEnum := make([]string, 0, len(t.providers))
	for _, p := range t.providers {
		if p.Model != "" {
			modelEnum = append(modelEnum, p.Model)
		}
	}

	return mcp.ToolDefinition{
		Name:        "llm",
		Description: "Call a large language model to analyze problems or generate responses. Select provider and model dynamically, or use defaults from config.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"prompt": {
					"type": "string",
					"description": "The prompt or question to send to the LLM"
				},
				"provider": {
					"type": "string",
					"description": "Provider name (e.g., 'openai', 'anthropic'). If not specified, uses the default provider."
				},
				"model": {
					"type": "string",
					"description": "Model name (e.g., 'deepseek-v4-flash', 'claude-3-5-sonnet'). If not specified, uses the default model."
				},
				"system": {
					"type": "string",
					"description": "Optional system prompt to guide the model's behavior"
				},
				"max_tokens": {
					"type": "integer",
					"description": "Maximum tokens in response (default 4096)"
				}
			},
			"required": ["prompt"]
		}`),
		Priority: 5,
		Source:   "built-in",
	}
}

func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var args struct {
		Prompt    string `json:"prompt"`
		Provider  string `json:"provider,omitempty"`
		Model     string `json:"model,omitempty"`
		System    string `json:"system,omitempty"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}

	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	if args.Prompt == "" {
		return &mcp.ToolResult{Content: "prompt is required", IsError: true}, nil
	}

	// Select provider
	provCfg := t.selectProvider(args.Provider, args.Model)
	if provCfg == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("no provider available for %s/%s", args.Provider, args.Model), IsError: true}, nil
	}

	// Create provider instance
	prov := provider.NewProviderFromConfig(provCfg)

	// Build request
	maxTokens := args.MaxTokens
	if maxTokens <= 0 {
		maxTokens = provCfg.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 4096
		}
	}

	model := args.Model
	if model == "" {
		model = provCfg.Model
	}

	req := provider.ProviderRequest{
		Messages: []provider.Message{
			{Role: "user", Content: provider.TextContent(args.Prompt)},
		},
		System:    args.System,
		MaxTokens: maxTokens,
		Model:     model,
	}

	// Execute
	resp, err := prov.Complete(ctx, req)
	if err != nil {
		zap.S().Errorw("llm tool: provider.Complete failed", "error", err, "provider", provCfg.Name)
		return &mcp.ToolResult{Content: fmt.Sprintf("LLM call failed: %v", err), IsError: true}, nil
	}

	// Extract text from response
	var blocks []map[string]any
	if err := json.Unmarshal(resp.Content, &blocks); err != nil {
		return &mcp.ToolResult{Content: string(resp.Content), IsError: false}, nil
	}

	var sb strings.Builder
	for _, block := range blocks {
		if text, ok := block["text"].(string); ok {
			sb.WriteString(text)
		}
	}

	result := sb.String()
	if result == "" {
		result = string(resp.Content)
	}

	data, _ := json.Marshal(map[string]any{
		"provider": provCfg.Name,
		"model":    model,
		"response": result,
		"usage":    resp.Usage,
	})

	return &mcp.ToolResult{Content: string(data)}, nil
}

// selectProvider returns the provider config matching the given provider/model hints,
// or the default provider if no hints given.
func (t *Tool) selectProvider(providerHint, modelHint string) *config.ProviderConfig {
	// If both hints specified, find exact match
	if providerHint != "" && modelHint != "" {
		for i := range t.providers {
			p := &t.providers[i]
			if p.Name == providerHint && (modelHint == "" || p.Model == modelHint) {
				return p
			}
		}
		return nil
	}

	// If provider hint only, find by name
	if providerHint != "" {
		for i := range t.providers {
			p := &t.providers[i]
			if p.Name == providerHint {
				return p
			}
		}
		return nil
	}

	// If model hint only, find by model
	if modelHint != "" {
		for i := range t.providers {
			p := &t.providers[i]
			if p.Model == modelHint {
				return p
			}
		}
	}

	// Default: first provider
	if len(t.providers) > 0 {
		return &t.providers[0]
	}
	return nil
}
