package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dolphin/internal/config"
)

func TestExecuteIflow_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"success": true,
			"code": "200",
			"message": "操作成功",
			"data": {
				"organic": [
					{
						"title": "Go语言并发编程",
						"link": "https://example.com/go-concurrency",
						"snippet": "Go并发编程入门教程"
					},
					{
						"title": "Goroutine详解",
						"link": "https://example.com/goroutine",
						"snippet": "深入了解goroutine"
					}
				],
				"query": "Go并发"
			}
		}`))
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "iflow"
	cfg.MCP.WebSearch.APIKey = "test-key"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mockReq, _ := http.NewRequest("POST", ts.URL, req.Body)
		mockReq.Header = req.Header
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{"query": "Go并发"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Go语言并发编程") {
		t.Fatalf("expected 'Go语言并发编程', got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://example.com/go-concurrency") {
		t.Fatalf("expected URL in result, got: %s", result.Content)
	}
}

func TestExecuteIflow_MissingKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "iflow"
	cfg.MCP.WebSearch.APIKey = ""
	tool := New(cfg)

	input, _ := json.Marshal(map[string]any{"query": "test"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for missing API key, got: %s", result.Content)
	}
}

func TestExecuteIflow_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"success": false,
			"code": "500",
			"message": "内部错误",
			"data": null
		}`))
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "iflow"
	cfg.MCP.WebSearch.APIKey = "test-key"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mockReq, _ := http.NewRequest("POST", ts.URL, req.Body)
		mockReq.Header = req.Header
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{"query": "test"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError for API error, got: %s", result.Content)
	}
}
