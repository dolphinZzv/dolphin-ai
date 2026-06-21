package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/types"
)

func init() {
	RegisterProvider("anthropic", func(cfg Config, logger *zap.Logger) Provider {
		return &anthropicProvider{cfg: cfg, logger: logger}
	})
}

type anthropicProvider struct {
	cfg    Config
	logger *zap.Logger
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "anthropic",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

func (p *anthropicProvider) chatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return baseURL + "/v1/messages"
}

func (p *anthropicProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	messages := BuildAnthropicMessages(req, p.logger)
	body, err := BuildAnthropicRequest(req.Model, messages, p.cfg, req)
	if err != nil {
		return nil, err
	}
	url := p.chatURL(p.cfg.BaseURL)
	if req.Stream {
		return StreamAnthropic(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
	}
	return CompleteAnthropic(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
}

// ---------------------------------------------------------------------------
// Exported types for vendor packages
// ---------------------------------------------------------------------------

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type AnthropicRequest struct {
	Model        string             `json:"model"`
	Messages     []AnthropicMessage `json:"messages"`
	System       string             `json:"system,omitempty"`
	MaxTokens    int                `json:"max_tokens"`
	Temperature  float64            `json:"temperature"`
	TopP         float64            `json:"top_p,omitempty"`
	Stream       bool               `json:"stream"`
	Stop         []string           `json:"stop_sequences,omitempty"`
	Tools        []AnthropicTool    `json:"tools,omitempty"`
	OutputConfig *OutputConfig      `json:"output_config,omitempty"`
	Thinking     *AnthropicThinking `json:"thinking,omitempty"`
}

type OutputConfig struct {
	Effort string `json:"effort"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicError struct {
	Message string `json:"message"`
}

type AnthropicErrorBody struct {
	Error AnthropicError `json:"error"`
}

// ---------------------------------------------------------------------------
// Exported helpers
// ---------------------------------------------------------------------------

// AnthropicChatURL constructs the messages URL from a base URL.
func AnthropicChatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return baseURL + "/v1/messages"
}

// BuildAnthropicMessages converts an LLMRequest to an Anthropic-compatible message array.
func BuildAnthropicMessages(req LLMRequest, logger *zap.Logger) []AnthropicMessage {
	var msgs []AnthropicMessage
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role { //nolint:exhaustive // RoleUser/RoleSystem share the default text path
		case types.RoleTool:
			// Collect all consecutive tool_result blocks into a single
			// user message so every tool_use from the preceding assistant
			// message has its result in the immediately following message.
			var blocks []map[string]any
			for j := i; j < len(req.Messages); j++ {
				tm := req.Messages[j]
				if tm.Role != types.RoleTool {
					break
				}
				block := map[string]any{
					"type":        "tool_result",
					"tool_use_id": tm.ToolCallID,
					"content":     tm.Content,
				}
				if tm.IsError {
					block["is_error"] = true
				}
				blocks = append(blocks, block)
				i = j
			}
			data, _ := json.Marshal(blocks)
			msgs = append(msgs, AnthropicMessage{Role: "user", Content: data})

		case types.RoleAssistant:
			if logger != nil {
				logger.Info("llm build assistant msg",
					zap.Bool("has_thinking", m.Thinking != ""),
					zap.Int("thinking_len", len(m.Thinking)),
					zap.Bool("has_signature", m.ThinkingSignature != ""),
					zap.Bool("has_content", m.Content != ""),
					zap.Int("tool_calls", len(m.ToolCalls)),
				)
			}
			if m.Thinking != "" || len(m.ToolCalls) > 0 {
				var blocks []map[string]any
				if m.Thinking != "" {
					block := map[string]any{"type": "thinking", "thinking": m.Thinking}
					if m.ThinkingSignature != "" {
						block["signature"] = m.ThinkingSignature
					}
					blocks = append(blocks, block)
				}
				if m.Content != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var input any = map[string]any{}
					if tc.Arguments != "" {
						_ = json.Unmarshal([]byte(tc.Arguments), &input)
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": input,
					})
				}
				data, _ := json.Marshal(blocks)
				msgs = append(msgs, AnthropicMessage{Role: "assistant", Content: data})
			} else {
				data, _ := json.Marshal(m.Content)
				msgs = append(msgs, AnthropicMessage{Role: "assistant", Content: data})
			}

		default:
			data, _ := json.Marshal(m.Content)
			msgs = append(msgs, AnthropicMessage{Role: string(m.Role), Content: data})
		}
	}
	return msgs
}

// BuildAnthropicTools converts ToolDef to Anthropic tool definitions.
func BuildAnthropicTools(tools []types.ToolDef) []AnthropicTool {
	out := make([]AnthropicTool, len(tools))
	for i, t := range tools {
		out[i] = AnthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: defaultSchema(t.Schema),
		}
	}
	return out
}

// BuildAnthropicRequest marshals a full Anthropic messages request body.
func BuildAnthropicRequest(model string, messages []AnthropicMessage, cfg Config, req LLMRequest) ([]byte, error) {
	temperature := req.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	if temperature == 0 {
		temperature = 1.0
	}

	body := AnthropicRequest{
		Model:       model,
		Messages:    messages,
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Stop:        req.Stop,
	}
	if req.ReasoningEffort != "" {
		body.OutputConfig = &OutputConfig{Effort: req.ReasoningEffort}
	}
	if req.Thinking {
		budget := req.MaxTokens
		if budget == 0 {
			budget = 4096
		}
		body.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: budget}
	}
	if len(req.Tools) > 0 {
		body.Tools = BuildAnthropicTools(req.Tools)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}
	return data, nil
}

// StreamAnthropic sends an HTTP POST request and streams the SSE response.
func StreamAnthropic(ctx context.Context, url, apiKey string, headers map[string]string, body []byte, timeout time.Duration, logger *zap.Logger) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk)

	logger.Debug("anthropic stream request", zap.String("url", url), zap.String("body", string(body)))

	go func() {
		defer close(ch)

		// send sends a chunk to ch or aborts if ctx is done, preventing
		// goroutine leaks when the consumer stops reading.
		send := func(chunk LLMChunk) (ok bool) {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			send(LLMChunk{Error: fmt.Errorf("llm: create request: %w", err)})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}

		logger.Debug("anthropic http request",
			zap.String("url", url),
			zap.Bool("has_api_key", apiKey != ""),
			zap.Int("api_key_len", len(apiKey)),
		)

		cl := &http.Client{Timeout: timeout}
		resp, err := cl.Do(httpReq)
		if err != nil {
			send(LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)})
			return
		}
		defer func() { _ = resp.Body.Close() }()

		cleanup := context.AfterFunc(ctx, func() { resp.Body.Close() })
		defer cleanup()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			var apiErr AnthropicErrorBody
			if json.Unmarshal(errBody, &apiErr) == nil && apiErr.Error.Message != "" {
				bodyPreview := string(body)
				if len(bodyPreview) > 2000 {
					bodyPreview = bodyPreview[:2000] + "..."
				}
				send(LLMChunk{Error: fmt.Errorf("llm: %s (status %d)\nrequest: %s", apiErr.Error.Message, resp.StatusCode, bodyPreview)})
			} else {
				send(LLMChunk{Error: fmt.Errorf("llm: status %d", resp.StatusCode)})
			}
			return
		}

		dec := NewAnthropicDecoder(resp.Body)
		for {
			chunk, err := dec.Decode()
			if errors.Is(err, io.EOF) {
				send(LLMChunk{Done: true})
				return
			}
			if err != nil {
				send(LLMChunk{Error: fmt.Errorf("llm: decode: %w", err)})
				return
			}
			if !send(chunk) {
				return
			}
		}
	}()

	return ch, nil
}

// anthropicNonStreamResponse is an Anthropic non-streaming messages response.
type anthropicNonStreamResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage,omitempty"`
}

// CompleteAnthropic sends a non-streaming HTTP POST and returns one chunk.
func CompleteAnthropic(ctx context.Context, url, apiKey string, headers map[string]string, body []byte, timeout time.Duration, logger *zap.Logger) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk, 1)

	go func() {
		defer close(ch)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: create request: %w", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}

		cl := &http.Client{Timeout: timeout}
		resp, err := cl.Do(httpReq)
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			var apiErr AnthropicErrorBody
			if json.Unmarshal(errBody, &apiErr) == nil && apiErr.Error.Message != "" {
				ch <- LLMChunk{Error: fmt.Errorf("llm: %s (status %d)", apiErr.Error.Message, resp.StatusCode)}
			} else {
				ch <- LLMChunk{Error: fmt.Errorf("llm: status %d (body: %s)", resp.StatusCode, string(errBody))}
			}
			return
		}

		var cr anthropicNonStreamResponse
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: decode response: %w", err)}
			return
		}

		chunk := LLMChunk{}
		if cr.Usage != nil {
			chunk.InputTokens = cr.Usage.InputTokens
			chunk.OutputTokens = cr.Usage.OutputTokens
			chunk.CacheCreationInputTokens = cr.Usage.CacheCreationInputTokens
			chunk.CacheReadInputTokens = cr.Usage.CacheReadInputTokens
		}
		for _, block := range cr.Content {
			switch block.Type {
			case "text":
				chunk.Content += block.Text
			case "thinking":
				chunk.Thinking = block.Thinking
				chunk.ThinkingSignature = block.Signature
			case "tool_use":
				args := ""
				if block.Input != nil {
					args = string(block.Input)
				}
				chunk.ToolCalls = append(chunk.ToolCalls, types.ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: args,
				})
			}
		}
		chunk.Done = true
		ch <- chunk
	}()

	return ch, nil
}

// AnthropicModelsURL constructs the models list URL from a base URL.
func AnthropicModelsURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	trimmed := strings.TrimRight(baseURL, "/")
	return trimmed + "/v1/models"
}

// anthropicModelsListResponse is the response from Anthropic's GET /v1/models endpoint.
type anthropicModelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverAnthropicModels calls the Anthropic /v1/models endpoint and returns the model list.
func DiscoverAnthropicModels(ctx context.Context, cfg Config) ([]ModelConfig, error) {
	url := AnthropicModelsURL(cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("llm: discover models: %w", err)
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: discover models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm: discover models: %s (status %d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	var result anthropicModelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("llm: discover models: decode: %w", err)
	}

	models := make([]ModelConfig, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelConfig{
			Name:    m.ID,
			Model:   m.ID,
			Vendor:  cfg.Vendor,
			APIType: cfg.APIType,
		})
	}
	return models, nil
}
