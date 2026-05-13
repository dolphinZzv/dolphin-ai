package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompleteStreamWithThinking(t *testing.T) {
	// Mock DeepSeek streaming server that sends reasoning_content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request body
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request: %v", err)
		}

		// Verify model
		if model, ok := reqBody["model"].(string); !ok || model != "deepseek-test" {
			t.Errorf("expected model deepseek-test, got %v", reqBody["model"])
		}

		// Verify stream is true
		if stream, ok := reqBody["stream"].(bool); !ok || !stream {
			t.Error("expected stream: true")
		}

		// Verify messages include reasoning_content when thinking blocks exist
		if msgs, ok := reqBody["messages"].([]any); ok {
			for _, m := range msgs {
				msg, _ := m.(map[string]any)
				if msg != nil && msg["role"] == "assistant" {
					if rc, hasRC := msg["reasoning_content"]; hasRC {
						t.Logf("assistant message has reasoning_content: %q", rc)
					}
				}
			}
		}

		// Stream DeepSeek-style SSE response with reasoning_content
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Phase 1: thinking
		chunks := []string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Let me analyze the question"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning_content":" step by step"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning_content":"\\nFirst, I need to check the data"}}]}`,
			// Phase 2: content starts
			`data: {"choices":[{"index":0,"delta":{"content":"Here is my analysis"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":" based on the information provided."}}]}`,
			// End
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "%s\n", c)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	provider := &OpenAIProvider{
		model:   "deepseek-test",
		maxTok:  4096,
		name:    "test",
		temp:    0.7,
		baseURL: server.URL,
		httpDoer: http.DefaultClient,
	}

	// Request WITHOUT thinking blocks in history (first call)
	req := ProviderRequest{
		System: "You are helpful",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := provider.CompleteStream(ctx, req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	// Collect chunks
	var chunks []StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	// Verify we got thinking blocks
	hasThinking := false
	hasContent := false
	for _, c := range chunks {
		if c.DeltaType == "thinking" && c.BlockDelta != "" {
			hasThinking = true
		}
		if len(c.Content) > 0 {
			hasContent = true
		}
		if c.Done {
			if !hasThinking {
				t.Error("expected thinking blocks before DONE")
			}
			if !hasContent {
				t.Error("expected content before DONE")
			}
		}
	}
}

func TestOpenAICompleteStreamWithThinkingInHistory(t *testing.T) {
	// Mock DeepSeek streaming server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that reasoning_content is included in the request
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request: %v", err)
		}

		if msgs, ok := reqBody["messages"].([]any); ok {
			foundReasoning := false
			for _, m := range msgs {
				msg, _ := m.(map[string]any)
				if msg != nil && msg["role"] == "assistant" {
					if rc, hasRC := msg["reasoning_content"]; hasRC {
						foundReasoning = true
						t.Logf("found reasoning_content: %q", rc)
					}
				}
			}
			if !foundReasoning {
				t.Error("expected reasoning_content in assistant messages but none found")
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n")
		fmt.Fprintf(w, "data: [DONE]\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	provider := &OpenAIProvider{
		model:    "deepseek-test",
		maxTok:   4096,
		name:     "test",
		temp:     0.7,
		baseURL:  server.URL,
		apiKey:   "test-key",
		httpDoer: http.DefaultClient,
	}

	// Request WITH thinking blocks in history (simulating a follow-up turn)
	req := ProviderRequest{
		System: "You are helpful",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"what is 2+2?"`)},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","thinking":"let me calculate 2+2"},
					{"type":"text","text":"the answer is 4"}
				]`),
			},
			{Role: "user", Content: json.RawMessage(`"and 3+3?"`)},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := provider.CompleteStream(ctx, req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	for c := range ch {
		if c.Done {
			break
		}
	}
}

func TestOpenAICompleteStreamBadRequest(t *testing.T) {
	// Simulate DeepSeek returning 400 with error message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"The reasoning_content must be passed back","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	provider := &OpenAIProvider{
		model:    "deepseek-test",
		maxTok:   4096,
		name:     "test",
		temp:     0.7,
		baseURL:  server.URL,
		apiKey:   "test-key",
		httpDoer: http.DefaultClient,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := provider.CompleteStream(ctx, ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})

	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "reasoning_content must be passed back") {
		t.Errorf("error should contain server message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}
