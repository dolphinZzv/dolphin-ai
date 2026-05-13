package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/metrics"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	client *openai.Client
	model  string
	maxTok int
	temp   float64
	name   string

	// httpDoer provides the Do(*http.Request) method we need.
	// We use the interface rather than *http.Client because go-openai's
	// config exposes an HTTPDoer interface (which *http.Client satisfies).
	httpDoer interface {
		Do(req *http.Request) (*http.Response, error)
	}
	baseURL    string
	apiKey     string
}

func NewOpenAIProvider(cfg *config.ProviderConfig) *OpenAIProvider {
	conf := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		conf.BaseURL = cfg.BaseURL
	}

	zap.S().Infow("openai provider created",
		"name", cfg.Name,
		"base_url", cfg.BaseURL,
		"model", cfg.Model,
	)

	return &OpenAIProvider{
		client:     openai.NewClientWithConfig(conf),
		model:      cfg.Model,
		maxTok:     cfg.MaxTokens,
		name:       cfg.Name,
		httpDoer: conf.HTTPClient,
		baseURL:    conf.BaseURL,
		apiKey:     cfg.APIKey,
	}
}

func (p *OpenAIProvider) Type() ProviderType { return ProviderOpenAI }
func (p *OpenAIProvider) Name() string       { return p.name }

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	return err
}

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

	reqBody := map[string]any{
		"model":       p.model,
		"max_tokens":  p.maxTok,
		"messages":    p.buildMessagesRaw(req),
		"temperature": p.temp,
		"stream":      true,
	}
	if tools := p.buildTools(req.Tools); len(tools) > 0 {
		reqBody["tools"] = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		timer.Stop()
		llmErrors.Inc()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		timer.Stop()
		llmErrors.Inc()
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.baseURL == "https://api.anthropic.com/v1" {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := p.httpDoer.Do(httpReq)
	if err != nil {
		timer.Stop()
		llmErrors.Inc()
		return nil, fmt.Errorf("openai stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		timer.Stop()
		llmErrors.Inc()
		errMsg := string(bodyBytes)
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		return nil, fmt.Errorf("openai stream: status %d, body: %s", resp.StatusCode, errMsg)
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		defer timer.Stop()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		// sseChunk holds the raw delta fields including provider-specific
		// extensions like DeepSeek's reasoning_content.
		type sseDelta struct {
			Content          string          `json:"content,omitempty"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			Role             string          `json:"role,omitempty"`
			ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
		}
		type sseChoice struct {
			Index int      `json:"index"`
			Delta sseDelta `json:"delta"`
		}
		type sseChunk struct {
			Choices []sseChoice `json:"choices"`
		}

		for scanner.Scan() {
			line := scanner.Text()

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}

			var chunk sseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				zap.S().Debugw("openai stream: skip unparseable chunk", "error", err)
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta

			sc := StreamChunk{}

			// reasoning_content → thinking block (DeepSeek)
			if delta.ReasoningContent != "" {
				sc.BlockDelta = delta.ReasoningContent
				sc.DeltaType = "thinking"
			}

			// Regular text content
			if delta.Content != "" {
				sc.Content = TextContent(delta.Content)
			}

			// Tool calls
			if delta.ToolCalls != nil {
				var toolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				}
				if err := json.Unmarshal(delta.ToolCalls, &toolCalls); err == nil {
					for _, tc := range toolCalls {
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
			}

			ch <- sc
		}

		if err := scanner.Err(); err != nil {
			zap.S().Errorw("openai stream: read error", "error", err)
		}
		ch <- StreamChunk{Done: true}
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
			// Parse content blocks for tool calls and thinking
			var blocks []map[string]any
			if err := json.Unmarshal(m.Content, &blocks); err == nil {
				var textParts []string
				for _, b := range blocks {
					switch b["type"] {
					case "text":
						if text, ok := b["text"].(string); ok {
							textParts = append(textParts, text)
						}
					case "thinking":
						// DeepSeek requires reasoning_content to be included
						// in assistant messages when reasoning was used.
						if thinking, ok := b["thinking"].(string); ok {
							// We include reasoning_content directly in the message
							// via the structured output, since go-openai doesn't
							// have a ReasoningContent field on ChatCompletionMessage.
							if msg.Content != "" {
								msg.Content += "\n"
							}
							msg.Content += thinking
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
				if len(textParts) > 0 {
					msg.Content = strings.Join(textParts, "\n")
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

func (p *OpenAIProvider) buildMessagesRaw(req ProviderRequest) []map[string]any {
	var msgs []map[string]any

	if req.System != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, map[string]any{
				"role":    "user",
				"content": string(m.Content),
			})
		case "assistant":
			msg := map[string]any{
				"role": "assistant",
			}

			var blocks []map[string]any
			if err := json.Unmarshal(m.Content, &blocks); err != nil {
				msg["content"] = string(m.Content)
				msgs = append(msgs, msg)
				continue
			}

			var textParts []string
			var reasoningParts []string
			var toolCalls []any
			for _, b := range blocks {
				switch b["type"] {
				case "text":
					if text, ok := b["text"].(string); ok {
						textParts = append(textParts, text)
					}
				case "thinking":
					if thinking, ok := b["thinking"].(string); ok {
						reasoningParts = append(reasoningParts, thinking)
					}
				case "tool_use":
					id, _ := b["id"].(string)
					name, _ := b["name"].(string)
					input := b["input"]
					inputJSON, _ := json.Marshal(input)
					toolCalls = append(toolCalls, map[string]any{
						"id":   id,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": string(inputJSON),
						},
					})
				}
			}

			if len(textParts) > 0 {
				msg["content"] = strings.Join(textParts, "\n")
			}
			if len(reasoningParts) > 0 {
				msg["reasoning_content"] = strings.Join(reasoningParts, "\n")
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			msgs = append(msgs, msg)

		case "tool":
			tcID := extractToolCallID(m.Content)
			content := extractToolResult(m.Content)
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"tool_call_id": tcID,
				"content":      content,
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
