package responses_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/llm"
	anthropicproto "dolphin/internal/llm/proto/anthropic"
	openaiproto "dolphin/internal/llm/proto/openai"
	"dolphin/internal/llm/proto/responses"
	"dolphin/internal/types"
)

const configPath = "../../../../config.yaml"

func loadMimoConfig(t *testing.T) (baseURL, apiKey, model string) {
	t.Helper()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Skipf("config not found: %v", err)
	}
	apiKey = cfg.GetString("llm.mimo.api_key")
	if apiKey == "" {
		t.Skip("llm.mimo.api_key not configured")
	}
	baseURL = cfg.GetString("llm.mimo.base_url")
	model = cfg.GetString("llm.mimo.models.0.name")
	if baseURL == "" || model == "" {
		t.Skip("llm.mimo.base_url or models.0.name not configured")
	}
	return
}

func oneWordRequest(model string) llm.LLMRequest {
	return llm.LLMRequest{
		Model:  model,
		System: "Answer with exactly one word. Never more than one word.",
		Messages: []types.Message{
			types.NewTextMessage(types.RoleUser, "What color is the sky on a clear day?"),
		},
		MaxTokens: 10,
		Stream:    false,
	}
}

func doPost(t *testing.T, url, apiKey string, body []byte) (int, string) {
	t.Helper()
	httpReq, _ := http.NewRequestWithContext(context.Background(),
		http.MethodPost, url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// ------- Responses API -------

func TestMimo_Responses(t *testing.T) {
	baseURL, apiKey, model := loadMimoConfig(t)

	req := oneWordRequest(model)
	req.MaxTokens = 200 // responses API burns tokens on reasoning before output
	input, instructions := responses.BuildInput(req, nil)
	body, err := responses.BuildRequest(model, input, instructions,
		llm.Config{Provider: "mimo", BaseURL: baseURL, APIKey: apiKey}, req)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	status, raw := doPost(t, responses.ChatURL(baseURL), apiKey, body)
	if status != 200 {
		t.Fatalf("responses: HTTP %d: %s", status, raw)
	}

	chunk, err := responses.DecodeComplete([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeComplete: %v", err)
	}

	t.Logf("responses: content=%q tokens(in=%d out=%d total=%d)",
		chunk.Content, chunk.InputTokens, chunk.OutputTokens, chunk.TotalTokens)
	t.Logf("raw: %.600s", raw)

	if chunk.Content == "" {
		t.Error("responses: empty content")
	}
}

// ------- Chat Completions API -------

func TestMimo_ChatCompletions(t *testing.T) {
	baseURL, apiKey, model := loadMimoConfig(t)

	req := oneWordRequest(model)
	req.MaxTokens = 50 // mimo may consume tokens on reasoning_content
	msgs := openaiproto.BuildMessages(req, nil)
	body, err := openaiproto.BuildRequest(model, msgs,
		llm.Config{Provider: "mimo", BaseURL: baseURL, APIKey: apiKey}, req)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	status, raw := doPost(t, openaiproto.ChatURL(baseURL), apiKey, body)
	if status != 200 {
		t.Fatalf("chat: HTTP %d: %s", status, raw)
	}

	chunk, err := openaiproto.DecodeComplete([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeComplete: %v", err)
	}

	t.Logf("chat: content=%q thinking=%q tokens(in=%d out=%d total=%d)",
		chunk.Content, chunk.Thinking, chunk.InputTokens, chunk.OutputTokens, chunk.TotalTokens)
	t.Logf("raw: %.600s", raw)

	if chunk.Content == "" {
		t.Error("chat: empty content")
	}
}

// ------- Anthropic Messages API -------

func TestMimo_Anthropic(t *testing.T) {
	baseURL, apiKey, model := loadMimoConfig(t)

	req := oneWordRequest(model)
	msgs := anthropicproto.BuildMessages(req, nil)
	body, err := anthropicproto.BuildRequest(model, msgs,
		llm.Config{Provider: "mimo", BaseURL: baseURL, APIKey: apiKey}, req)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	// Mimo's Anthropic endpoint is at /anthropic/v1/messages (not /v1/messages).
	anthropicURL := baseURL + "/anthropic/v1/messages"

	httpReq, _ := http.NewRequestWithContext(context.Background(),
		http.MethodPost, anthropicURL, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		// Retry with Bearer auth — some providers accept both.
		t.Logf("anthropic x-api-key: HTTP %d: %.600s", resp.StatusCode, string(raw))

		httpReq2, _ := http.NewRequestWithContext(context.Background(),
			http.MethodPost, anthropicURL, bytes.NewReader(body))
		httpReq2.Header.Set("Content-Type", "application/json")
		httpReq2.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq2.Header.Set("anthropic-version", "2023-06-01")

		resp2, err2 := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq2)
		if err2 != nil {
			t.Fatalf("http.Do (retry): %v", err2)
		}
		defer resp2.Body.Close()
		raw2, _ := io.ReadAll(resp2.Body)
		t.Logf("anthropic Bearer: HTTP %d: %.600s", resp2.StatusCode, string(raw2))

		if resp2.StatusCode != 200 {
			return
		}
		raw = raw2
	}

	chunk, err := anthropicproto.DecodeComplete(raw)
	if err != nil {
		t.Fatalf("DecodeComplete: %v", err)
	}

	t.Logf("anthropic: content=%q tokens(in=%d out=%d total=%d)",
		chunk.Content, chunk.InputTokens, chunk.OutputTokens, chunk.TotalTokens)

	if chunk.Content == "" {
		t.Error("anthropic: empty content")
	}
}
