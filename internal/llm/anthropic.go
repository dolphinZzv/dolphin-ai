package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dolphin/internal/types"
	"go.uber.org/zap"
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
	return StreamAnthropic(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
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
}

type OutputConfig struct {
	Effort string `json:"effort"`
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
	for _, m := range req.Messages {
		switch m.Role {
		case types.RoleTool:
			blocks := []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				},
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
		Stream:      true,
		Stop:        req.Stop,
	}
	if req.ReasoningEffort != "" {
		body.OutputConfig = &OutputConfig{Effort: req.ReasoningEffort}
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

		logger.Debug("anthropic http request",
			zap.String("url", url),
			zap.Bool("has_api_key", apiKey != ""),
			zap.Int("api_key_len", len(apiKey)),
		)

		cl := &http.Client{Timeout: timeout}
		resp, err := cl.Do(httpReq)
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)}
			return
		}
		defer resp.Body.Close()

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
				ch <- LLMChunk{Error: fmt.Errorf("llm: %s (status %d)\nrequest: %s", apiErr.Error.Message, resp.StatusCode, bodyPreview)}
			} else {
				ch <- LLMChunk{Error: fmt.Errorf("llm: status %d", resp.StatusCode)}
			}
			return
		}

		dec := NewAnthropicDecoder(resp.Body)
		for {
			chunk, err := dec.Decode()
			if err == io.EOF {
				ch <- LLMChunk{Done: true}
				return
			}
			if err != nil {
				ch <- LLMChunk{Error: fmt.Errorf("llm: decode: %w", err)}
				return
			}
			ch <- chunk
		}
	}()

	return ch, nil
}
