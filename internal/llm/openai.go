package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dolphin/internal/types"
	"go.uber.org/zap"
)

func init() {
	RegisterProvider("openai", func(cfg Config, logger *zap.Logger) Provider {
		return &openAIProvider{cfg: cfg, logger: logger}
	})
}

type openAIProvider struct {
	cfg    Config
	logger *zap.Logger
}

func (p *openAIProvider) Name() string { return "openai" }

func (p *openAIProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "openai",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

func (p *openAIProvider) chatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return baseURL + "/v1/chat/completions"
}

func (p *openAIProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	messages := BuildOpenAIMessages(req, p.logger)
	body, err := BuildOpenAIRequest(req.Model, messages, p.cfg, req)
	if err != nil {
		return nil, err
	}
	url := p.chatURL(p.cfg.BaseURL)
	return StreamOpenAI(ctx, url, p.cfg.APIKey, p.cfg.Headers, body, req.Timeout, p.logger)
}

// ---------------------------------------------------------------------------
// Exported types for vendor packages
// ---------------------------------------------------------------------------

type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
}

type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type OpenAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// Exported helpers
// ---------------------------------------------------------------------------

// OpenAIChatURL constructs the chat completions URL from a base URL.
func OpenAIChatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	apiPath := "/v1/chat/completions"
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") {
		apiPath = "/chat/completions"
	}
	return baseURL + apiPath
}

// BuildOpenAIMessages converts an LLMRequest to an OpenAI-compatible message array.
func BuildOpenAIMessages(req LLMRequest, logger *zap.Logger) []OpenAIMessage {
	var msgs []OpenAIMessage
	if req.System != "" {
		msgs = append(msgs, OpenAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case types.RoleTool:
			msgs = append(msgs, OpenAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		case types.RoleAssistant:
			if logger != nil {
				logger.Info("build openai assistant msg",
					zap.Bool("has_content", m.Content != ""),
					zap.Int("tool_calls", len(m.ToolCalls)),
				)
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]OpenAIToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					tcs[i] = OpenAIToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				msgs = append(msgs, OpenAIMessage{
					Role:      "assistant",
					Content:   nil,
					ToolCalls: tcs,
				})
			} else {
				msgs = append(msgs, OpenAIMessage{Role: "assistant", Content: m.Content})
			}
		default:
			msgs = append(msgs, OpenAIMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	return msgs
}

// BuildOpenAITools converts ToolDef to OpenAI tool definitions.
func BuildOpenAITools(tools []types.ToolDef) []OpenAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]OpenAITool, len(tools))
	for i, t := range tools {
		out[i] = OpenAITool{
			Type: "function",
			Function: OpenAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  defaultSchema(t.Schema),
			},
		}
	}
	return out
}

// BuildOpenAIRequest marshals a full OpenAI chat completion request body.
func BuildOpenAIRequest(model string, messages []OpenAIMessage, cfg Config, req LLMRequest) ([]byte, error) {
	temperature := req.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	if temperature == 0 {
		temperature = 1.0
	}

	tools := BuildOpenAITools(req.Tools)
	body := struct {
		Model           string          `json:"model"`
		Messages        []OpenAIMessage `json:"messages"`
		Temperature     float64         `json:"temperature"`
		TopP            float64         `json:"top_p,omitempty"`
		MaxTokens       int             `json:"max_tokens"`
		Stream          bool            `json:"stream"`
		Stop            []string        `json:"stop,omitempty"`
		Tools           []OpenAITool    `json:"tools,omitempty"`
		ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	}{
		Model:           model,
		Messages:        messages,
		Temperature:     temperature,
		TopP:            req.TopP,
		MaxTokens:       req.MaxTokens,
		Stream:          true,
		Stop:            req.Stop,
		ReasoningEffort: req.ReasoningEffort,
	}
	if len(tools) > 0 {
		body.Tools = tools
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}
	return data, nil
}

// StreamOpenAI sends an HTTP POST request and streams the SSE response.
func StreamOpenAI(ctx context.Context, url, apiKey string, headers map[string]string, body []byte, timeout time.Duration, logger *zap.Logger) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk)

	if logger != nil {
		logger.Debug("openai stream request", zap.String("url", url), zap.String("body", string(body)))
	}

	go func() {
		defer close(ch)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: create request: %w", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}

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
			var apiErr OpenAIErrorBody
			if json.Unmarshal(errBody, &apiErr) == nil && apiErr.Error.Message != "" {
				ch <- LLMChunk{Error: fmt.Errorf("llm: %s (status %d)", apiErr.Error.Message, resp.StatusCode)}
			} else {
				ch <- LLMChunk{Error: fmt.Errorf("llm: status %d", resp.StatusCode)}
			}
			return
		}

		dec := NewStreamDecoder(resp.Body)
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

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func defaultSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schema
}

// OpenAIModelsURL constructs the models list URL from a base URL.
func OpenAIModelsURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}

// modelsListResponse is the response from OpenAI's GET /v1/models endpoint.
type modelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// DiscoverOpenAIModels calls the OpenAI-compatible /v1/models endpoint and returns the model list.
func DiscoverOpenAIModels(cfg Config) ([]ModelConfig, error) {
	url := OpenAIModelsURL(cfg.BaseURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("llm: discover models: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: discover models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm: discover models: %s (status %d)", strings.TrimSpace(string(body)), resp.StatusCode)
	}

	var result modelsListResponse
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
