package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"dolphinzZ/internal/config"
	"dolphinzZ/internal/metrics"

	"github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	client *openai.Client
	model  string
	maxTok int
	temp   float64
}

func NewOpenAIProvider(cfg *config.LLMConfig) *OpenAIProvider {
	conf := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		conf.BaseURL = cfg.BaseURL
	}

	slog.Info("openai provider created",
		"base_url", cfg.BaseURL,
		"model", cfg.Model,
	)

	return &OpenAIProvider{
		client: openai.NewClientWithConfig(conf),
		model:  cfg.Model,
		maxTok: cfg.MaxTokens,
	}
}

func (p *OpenAIProvider) Type() ProviderType { return ProviderOpenAI }

func (p *OpenAIProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	llmRequests.Inc()
	timer := metrics.StartTimer(llmDuration)
	defer timer.Stop()

	openAIReq := openai.ChatCompletionRequest{
		Model:       p.model,
		MaxTokens:   p.maxTok,
		Messages:    p.buildMessages(req),
		Tools:       p.buildTools(req.Tools),
		Temperature: float32(p.temp),
	}

	resp, err := p.client.CreateChatCompletion(ctx, openAIReq)
	if err != nil {
		llmErrors.Inc()
		return nil, fmt.Errorf("openai completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		llmErrors.Inc()
		return nil, fmt.Errorf("no choices in response")
	}

	llmInputTokens.Add(int64(resp.Usage.PromptTokens))
	llmOutputTokens.Add(int64(resp.Usage.CompletionTokens))

	choice := resp.Choices[0]
	msg := choice.Message

	providerResp := &ProviderResponse{
		Usage: &Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
		StopReason: string(choice.FinishReason),
	}

	// Check for tool calls
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			args := json.RawMessage(tc.Function.Arguments)
			providerResp.ToolCalls = append(providerResp.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
	}

	// Text content
	if msg.Content != "" {
		providerResp.Content = TextContent(msg.Content)
	}

	return providerResp, nil
}

func (p *OpenAIProvider) CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	llmRequests.Inc()
	timer := metrics.StartTimer(llmDuration)

	openAIReq := openai.ChatCompletionRequest{
		Model:       p.model,
		MaxTokens:   p.maxTok,
		Messages:    p.buildMessages(req),
		Tools:       p.buildTools(req.Tools),
		Temperature: float32(p.temp),
		Stream:      true,
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, openAIReq)
	if err != nil {
		timer.Stop()
		llmErrors.Inc()
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer stream.Close()
		defer timer.Stop()

		for {
			chunk, err := stream.Recv()
			if err != nil {
				ch <- StreamChunk{Done: true}
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta

			sc := StreamChunk{}
			if delta.Content != "" {
				sc.Content = TextContent(delta.Content)
			}
			if len(delta.ToolCalls) > 0 {
				for _, tc := range delta.ToolCalls {
					if tc.ID != "" {
						sc.ToolCallBegin = &ToolCallBegin{
							ID:   tc.ID,
							Name: tc.Function.Name,
						}
					}
					if tc.Function.Arguments != "" {
						sc.ToolCallDelta = tc.Function.Arguments
					}
				}
			}
			ch <- sc
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) buildMessages(req ProviderRequest) []openai.ChatCompletionMessage {
	var msgs []openai.ChatCompletionMessage

	// System prompt
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.System,
		})
	}

	// Conversation history
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: string(m.Content),
			})
		case "assistant":
			msg := openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant,
			}
			// Parse content blocks for tool calls
			var blocks []map[string]any
			if err := json.Unmarshal(m.Content, &blocks); err == nil {
				for _, b := range blocks {
					switch b["type"] {
					case "text":
						if text, ok := b["text"].(string); ok {
							msg.Content = text
						}
					case "tool_use":
						id, _ := b["id"].(string)
						name, _ := b["name"].(string)
						input := b["input"]
						inputJSON, _ := json.Marshal(input)
						msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
							ID:   id,
							Type: "function",
							Function: openai.FunctionCall{
								Name:      name,
								Arguments: string(inputJSON),
							},
						})
					}
				}
			}
			if msg.Content == "" && len(msg.ToolCalls) > 0 {
				msg.Content = ""
			}
			msgs = append(msgs, msg)

		case "tool":
			tcID := extractToolCallID(m.Content)
			content := extractToolResult(m.Content)
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				ToolCallID: tcID,
				Content:    content,
			})
		}
	}

	return msgs
}

func (p *OpenAIProvider) buildTools(defs []ToolDef) []openai.Tool {
	tools := make([]openai.Tool, 0, len(defs))
	for _, d := range defs {
		var schema map[string]any
		json.Unmarshal(d.InputSchema, &schema)

		tools = append(tools, openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  schema,
			},
		})
	}
	return tools
}

// extractToolCallID extracts the tool_call_id from tool result content.
func extractToolCallID(content json.RawMessage) string {
	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	for _, b := range blocks {
		if id, ok := b["tool_use_id"].(string); ok {
			return id
		}
	}
	return ""
}

// extractToolResult extracts text content from tool result blocks.
func extractToolResult(content json.RawMessage) string {
	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return string(content)
	}
	for _, b := range blocks {
		if b["type"] == "tool_result" {
			switch v := b["content"].(type) {
			case string:
				return v
			case []any:
				// Anthropic format: [{type: "text", text: "..."}]
				for _, item := range v {
					if m, ok := item.(map[string]any); ok {
						if t, ok := m["text"].(string); ok {
							return t
						}
					}
				}
			}
		}
	}
	return string(content)
}
