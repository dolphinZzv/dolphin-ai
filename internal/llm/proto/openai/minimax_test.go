package openai_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/llm"
	openaiproto "dolphin/internal/llm/proto/openai"
	"dolphin/internal/types"
)

func TestMinimax_ChatCompletions(t *testing.T) {
	cfg, err := config.LoadConfig("../../../../config.yaml")
	if err != nil {
		t.Skipf("config not found: %v", err)
	}
	apiKey := cfg.GetString("llm.minimax.api_key")
	if apiKey == "" {
		t.Skip("llm.minimax not configured")
	}

	baseURL := cfg.GetString("llm.minimax.base_url")
	model := cfg.GetString("llm.minimax.models.0.name")
	if model == "" {
		t.Skip("llm.minimax.models.0.name not configured")
	}
	// Fix user config if it has the wrong /openai/v1 prefix.
	baseURL = strings.Replace(baseURL, "/openai/v1", "/v1", 1)
	apiType := cfg.GetString("llm.minimax.api_type")

	t.Logf("minimax: base_url=%s model=%s api_type=%s", baseURL, model, apiType)

	req := llm.LLMRequest{
		Model:  model,
		System: "Answer with exactly one word. Never more than one word.",
		Messages: []types.Message{
			types.NewTextMessage(types.RoleUser, "What color is the sky on a clear day?"),
		},
		MaxTokens: 100, // minimax may consume tokens on think before output
		Stream:    false,
	}

	msgs := openaiproto.BuildMessages(req, nil)
	body, err := openaiproto.BuildRequest(model, msgs,
		llm.Config{Provider: "minimax", BaseURL: baseURL, APIKey: apiKey}, req)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	url := openaiproto.ChatURL(baseURL)
	t.Logf("ChatURL: %s", url)

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

	t.Logf("HTTP %d", resp.StatusCode)

	if resp.StatusCode != 200 {
		t.Logf("response: %.800s", string(raw))
		return
	}

	chunk, err := openaiproto.DecodeComplete(raw)
	if err != nil {
		t.Fatalf("DecodeComplete: %v", err)
	}

	t.Logf("content: %q | tokens: in=%d out=%d total=%d",
		chunk.Content, chunk.InputTokens, chunk.OutputTokens, chunk.TotalTokens)

	if chunk.Content == "" {
		t.Error("empty content")
	}
}
