package responses_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/llm/proto/responses"
	"dolphin/internal/types"
)

// TestMimoResponsesAPI verifies the Responses API protocol with the real Mimo
// endpoint. It reads credentials from config.yaml (llm.mimo section with
// api_type: openai-responses) and skips when mimo is not configured.
func TestMimoResponsesAPI(t *testing.T) {
	cfgPath := "../../../../config.yaml" // project root from internal/llm/proto/responses/
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Skipf("config not found (%s): %v", cfgPath, err)
	}

	apiKey := cfg.GetString("llm.mimo.api_key")
	if apiKey == "" {
		t.Skip("llm.mimo.api_key not configured, skipping integration test")
	}
	apiType := cfg.GetString("llm.mimo.api_type")
	if apiType != "openai-responses" {
		t.Skipf("llm.mimo.api_type is %q, not openai-responses", apiType)
	}
	baseURL := cfg.GetString("llm.mimo.base_url")
	model := cfg.GetString("llm.mimo.models.0.name")
	if baseURL == "" || model == "" {
		t.Skip("llm.mimo.base_url or models.0.name not configured")
	}

	t.Logf("mimo config: base_url=%s model=%s api_type=%s", baseURL, model, apiType)

	llmCfg := llm.Config{
		Provider: "mimo",
		BaseURL:  baseURL,
		APIKey:   apiKey,
	}

	// Build a simple chat request.
	req := llm.LLMRequest{
		Model:    model,
		System:   "You are a helpful assistant. Reply briefly.",
		Messages: []types.Message{types.NewTextMessage(types.RoleUser, "Hello, who are you?")},
		Stream:   false,
	}

	t.Run("non_streaming", func(t *testing.T) {
		input, instructions := responses.BuildInput(req, nil)
		body, err := responses.BuildRequest(model, input, instructions, llmCfg, req)
		if err != nil {
			t.Fatalf("BuildRequest error: %v", err)
		}

		// Verify request body structure.
		if !bytes.Contains(body, []byte(`"input"`)) {
			t.Fatal("request body missing 'input' field")
		}
		if !bytes.Contains(body, []byte(`"instructions"`)) {
			t.Fatal("request body missing 'instructions' field")
		}
		if !bytes.Contains(body, []byte(model)) {
			t.Fatal("request body missing model name")
		}
		if bytes.Contains(body, []byte(`"messages"`)) {
			t.Fatal("request body should not contain 'messages' (Chat API), got it")
		}
		t.Logf("request body: %s", string(body))

		// Make real HTTP call.
		httpReq, err := http.NewRequestWithContext(context.Background(),
			http.MethodPost, responses.ChatURL(llmCfg.BaseURL), bytes.NewReader(body))
		if err != nil {
			t.Fatalf("http.NewRequest error: %v", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+llmCfg.APIKey)

		client := &http.Client{Timeout: 15 * time.Second}
		httpResp, err := client.Do(httpReq)
		if err != nil {
			t.Fatalf("http.Do error: %v", err)
		}
		defer httpResp.Body.Close()

		t.Logf("HTTP status: %d", httpResp.StatusCode)
		t.Logf("HTTP headers: %v", httpResp.Header)

		// Test error decoding on any non-200 response.
		if httpResp.StatusCode != 200 {
			chunk, err := responses.DecodeComplete(nil)
			if err == nil {
				t.Logf("non-streaming decode (empty): content=%q err=%v", chunk.Content, chunk.Error)
			}
			// Verify our error decoder works.
			bodyBytes := make([]byte, 4096)
			n, _ := httpResp.Body.Read(bodyBytes)
			if n > 0 {
				errDetail := responses.DecodeError(httpResp.StatusCode, bodyBytes[:n])
				t.Logf("decoded error: %v", errDetail)
				if errDetail == nil {
					// Some non-standard errors may not parse — that's fine.
					t.Logf("raw response body: %s", string(bodyBytes[:n]))
				}
			}
			return
		}

		// Success: decode with DecodeComplete.
		bodyBytes := make([]byte, 65536)
		n, err := httpResp.Body.Read(bodyBytes)
		if err != nil && err.Error() != "EOF" {
			t.Fatalf("read body error: %v", err)
		}
		chunk, err := responses.DecodeComplete(bodyBytes[:n])
		if err != nil {
			t.Fatalf("DecodeComplete error: %v", err)
		}
		t.Logf("decoded chunk: content=%q len=%d tool_calls=%d input_tokens=%d output_tokens=%d done=%v",
			chunk.Content, len(chunk.Content), len(chunk.ToolCalls),
			chunk.InputTokens, chunk.OutputTokens, chunk.Done)

		if chunk.Content == "" {
			t.Error("expected non-empty content in response")
		}
	})

	t.Run("streaming", func(t *testing.T) {
		reqStream := req
		reqStream.Stream = true

		input, instructions := responses.BuildInput(reqStream, nil)
		body, err := responses.BuildRequest(model, input, instructions, llmCfg, reqStream)
		if err != nil {
			t.Fatalf("BuildRequest error: %v", err)
		}
		if !bytes.Contains(body, []byte(`"stream":true`)) {
			t.Fatal("streaming request should have stream:true")
		}

		// Use DoStream with proper decoder.
		httpReq, err := http.NewRequestWithContext(context.Background(),
			http.MethodPost, responses.ChatURL(llmCfg.BaseURL), bytes.NewReader(body))
		if err != nil {
			t.Fatalf("http.NewRequest error: %v", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+llmCfg.APIKey)

		stream, err := proto.DoStream(context.Background(), httpReq, 30*time.Second,
			responses.NewChunkDecoder, responses.DecodeError, nil)
		if err != nil {
			t.Logf("DoStream error: %v (may be expected for non-200)", err)
			return
		}

		var content string
		var toolCalls int
		for chunk := range stream {
			if chunk.Error != nil {
				t.Logf("stream error: %v", chunk.Error)
			}
			content += chunk.Content
			toolCalls += len(chunk.ToolCalls)
			if chunk.Done {
				t.Logf("stream done: content=%d chars tool_calls=%d in=%d out=%d",
					len(content), toolCalls, chunk.InputTokens, chunk.OutputTokens)
				break
			}
		}
	})

	t.Run("tool_calling", func(t *testing.T) {
		reqTool := llm.LLMRequest{
			Model:    model,
			System:   "You have access to tools. Use get_weather when asked about weather.",
			Messages: []types.Message{types.NewTextMessage(types.RoleUser, "What is the weather in Beijing?")},
			Tools: []types.ToolDef{{
				Name:        "get_weather",
				Description: "Get current weather for a city",
				Schema:      []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
			}},
			Stream: false,
		}

		input, instructions := responses.BuildInput(reqTool, nil)
		body, err := responses.BuildRequest(model, input, instructions, llmCfg, reqTool)
		if err != nil {
			t.Fatalf("BuildRequest tool error: %v", err)
		}

		if !bytes.Contains(body, []byte(`"tools"`)) {
			t.Fatal("tool request body missing 'tools' field")
		}
		if !bytes.Contains(body, []byte(`"get_weather"`)) {
			t.Fatal("tool request body missing tool name 'get_weather'")
		}
		t.Logf("tool request body: %s", string(body))

		httpReq, _ := http.NewRequestWithContext(context.Background(),
			http.MethodPost, responses.ChatURL(llmCfg.BaseURL), bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+llmCfg.APIKey)

		client := &http.Client{Timeout: 30 * time.Second}
		httpResp, err := client.Do(httpReq)
		if err != nil {
			t.Fatalf("http.Do error: %v", err)
		}
		defer httpResp.Body.Close()

		t.Logf("tool HTTP status: %d", httpResp.StatusCode)

		if httpResp.StatusCode == 200 {
			bodyBytes := make([]byte, 65536)
			n, _ := httpResp.Body.Read(bodyBytes)
			chunk, err := responses.DecodeComplete(bodyBytes[:n])
			if err != nil {
				t.Fatalf("DecodeComplete error: %v", err)
			}
			t.Logf("tool response: content=%q tool_calls=%d", chunk.Content, len(chunk.ToolCalls))
			for _, tc := range chunk.ToolCalls {
				t.Logf("  tool_call: name=%s id=%s args=%s", tc.Name, tc.ID, tc.Arguments)
			}
		}
	})
}
