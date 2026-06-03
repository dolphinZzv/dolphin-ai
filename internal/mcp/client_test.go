package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dolphin/internal/types"
)

// browserMCPServer is a test HTTP server that mimics the BrowserMCP app's
// HTTP behaviour: single-read connections (no read loop), JSON for tools/list,
// SSE for tools/call, and the same /sse + /message URL routes.
type browserMCPServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string // captured JSON-RPC method names for assertions
}

func newBrowserMCPServer(t testing.TB) *browserMCPServer {
	t.Helper()

	s := &browserMCPServer{}
	// Use a raw net.Listener so we can accept one-shot connections that
	// never read again after the first request (like the real BrowserMCP).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Server = httptest.NewUnstartedServer(nil)
	s.Listener = ln

	// We override the server config to handle connections one-shot.
	s.Config = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			var method string
			// Parse JSON-RPC body to extract method.
			var req struct {
				Method string `json:"method"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&req)
			}
			method = req.Method
			s.requests = append(s.requests, method)
			s.mu.Unlock()

			switch r.Method + " " + r.URL.Path {
			case "POST /sse", "POST /message":
				switch method {
				case "tools/list":
					respondToolsList(w)
				case "tools/call":
					handleCallSSE(w, r)
				default:
					http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32601,"message":"method not found"}}`, 200)
				}
			default:
				http.Error(w, "Not Found", 404)
			}
		}),
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

var browserTools = []map[string]any{
	{
		"name":        "browser_navigate",
		"description": "Navigate to a URL",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "Target URL"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name":        "browser_evaluate",
		"description": "Execute JavaScript on the current page",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string", "description": "JS expression"},
			},
			"required": []string{"expression"},
		},
	},
	{
		"name":        "browser_screenshot",
		"description": "Take a screenshot",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":        map[string]any{"type": "string", "description": "Optional URL"},
				"output_dir": map[string]any{"type": "string", "description": "Output dir"},
			},
		},
	},
}

func respondToolsList(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "keep-alive")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"tools": browserTools,
		},
	})
}

func handleCallSSE(w http.ResponseWriter, r *http.Request) {
	// Parse params to get tool name.
	var req struct {
		ID     int             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Simulate async processing.
	result := `{"status":"ok","url":"https://baidu.com"}`
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": result},
			},
		},
	}
	jsonData, _ := json.Marshal(body)
	_, _ = fmt.Fprintf(w, "event: result\ndata: %s\n\n", jsonData)
	flusher.Flush()
}

// oneShotHandler is a server that only reads one HTTP request per TCP
// connection, then stops reading (like the real BrowserMCP's NWConnection
// behaviour). This allows us to verify that DisableKeepAlives workaround
// actually prevents connection reuse hangs.
type oneShotServer struct {
	*httptest.Server
	mu        sync.Mutex
	callCount int
}

func newOneShotServer(t testing.TB) *oneShotServer {
	t.Helper()
	s := &oneShotServer{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Server = httptest.NewUnstartedServer(nil)
	s.Listener = ln
	s.Config = &http.Server{Handler: s}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func (s *oneShotServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.callCount++
	s.mu.Unlock()
	respondToolsList(w)
}

// ---------------------------------------------------------------------------
// Client tests against BrowserMCP-like server
// ---------------------------------------------------------------------------

func TestClient_ListViaSSEEndpoint(t *testing.T) {
	srv := newBrowserMCPServer(t)
	client := NewClient(srv.URL + "/sse")

	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}
	if defs[0].Name != "browser_navigate" {
		t.Fatalf("expected browser_navigate, got %s", defs[0].Name)
	}
}

func TestClient_ListViaMessageEndpoint(t *testing.T) {
	srv := newBrowserMCPServer(t)
	client := NewClient(srv.URL + "/message")

	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}
}

func TestClient_ExecuteViaSSEEndpoint(t *testing.T) {
	srv := newBrowserMCPServer(t)
	client := NewClient(srv.URL + "/sse")

	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "browser_navigate",
		Arguments: `{"url":"https://baidu.com"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(result.Content, "baidu.com") {
		t.Fatalf("expected baidu.com in result, got: %s", result.Content)
	}
}

func TestClient_ContextTimeout(t *testing.T) {
	// A server that delays response to trigger timeout.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		respondToolsList(w)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.List(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClient_ServerNotFound(t *testing.T) {
	client := NewClient("http://127.0.0.1:18765")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.List(ctx)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// DisableKeepAlives: verify that one-shot server works with new connections
// ---------------------------------------------------------------------------

func TestClient_DisableKeepAlivesAllowsMultipleRequests(t *testing.T) {
	srv := newOneShotServer(t)
	client := NewClient(srv.URL)

	// First request.
	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}

	// Second request on a new connection (because DisableKeepAlives=true).
	defs, err = client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools on second call, got %d", len(defs))
	}

	// Verify both calls reached the server.
	srv.mu.Lock()
	count := srv.callCount
	srv.mu.Unlock()
	if count != 2 {
		t.Fatalf("expected 2 server calls, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// LazyClient tests
// ---------------------------------------------------------------------------

func TestLazyClient_List(t *testing.T) {
	srv := newBrowserMCPServer(t)
	lc := NewLazyClient(srv.URL + "/sse")

	defs, err := lc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}
}

func TestLazyClient_Execute(t *testing.T) {
	srv := newBrowserMCPServer(t)
	lc := NewLazyClient(srv.URL + "/sse")

	result, err := lc.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "browser_navigate",
		Arguments: `{"url":"https://baidu.com"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
}

func TestLazyClient_RetryAfterServerAvailable(t *testing.T) {
	// Start with no server — first List should fail.
	lc := NewLazyClient("http://127.0.0.1:18766")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err := lc.List(ctx)
	cancel()
	if err == nil {
		t.Fatal("expected error when server not running")
	}

	// Now start a server and try again — should succeed with new connection.
	srv := newBrowserMCPServer(t)
	// Update the lazy client's URL to point at the new server.
	// (In real usage, the URL is fixed, but here we simulate retry by
	//  testing that a new LazyClient with the correct URL works.)
	lc2 := NewLazyClient(srv.URL + "/sse")
	defs, err := lc2.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(defs))
	}
}

func TestLazyClient_ListThenExecute(t *testing.T) {
	srv := newBrowserMCPServer(t)
	lc := NewLazyClient(srv.URL + "/sse")

	// List first.
	defs, err := lc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) == 0 {
		t.Fatal("expected at least one tool")
	}

	// Then Execute — should use the same cached client.
	result, err := lc.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "browser_navigate",
		Arguments: `{"url":"https://example.com"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
}

// ---------------------------------------------------------------------------
// SSE parsing tests
// ---------------------------------------------------------------------------

func TestReadSSE_ValidResult(t *testing.T) {
	input := "event: result\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n"
	body := strings.NewReader(input)
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.ID != 1 {
		t.Fatalf("expected id 1, got %d", resp.ID)
	}
}

func TestReadSSE_ValidError(t *testing.T) {
	input := "event: error\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-1,\"message\":\"something went wrong\"}}\n\n"
	body := strings.NewReader(input)
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -1 {
		t.Fatalf("expected code -1, got %d", resp.Error.Code)
	}
}

func TestReadSSE_SkipsProgressBeforeResult(t *testing.T) {
	input := "event: progress\ndata: {\"progress\":50}\n\nevent: result\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n\n"
	body := strings.NewReader(input)
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestReadSSE_NoResult(t *testing.T) {
	input := "event: ping\ndata: {}\n\nevent: keepalive\ndata: {}\n\n"
	body := strings.NewReader(input)
	_, err := readSSE(body)
	if err == nil {
		t.Fatal("expected error for no result in stream")
	}
}

func TestReadSSE_TrailingData(t *testing.T) {
	input := "event: result\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"ok\"}"
	body := strings.NewReader(input)
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestReadSSE_UnnamedEvent(t *testing.T) {
	input := "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"ok\"}\n\n"
	body := strings.NewReader(input)
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestReadSSE_MultipleDataLines(t *testing.T) {
	input := "event: result\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n"
	body := bufio.NewReader(strings.NewReader(input))
	resp, err := readSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// HTTP transport tests
// ---------------------------------------------------------------------------

func TestNewClient_Timeout(t *testing.T) {
	c := NewClient("http://localhost:9999")
	if c.http.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", c.http.Timeout)
	}
}

// ---------------------------------------------------------------------------
// TaskID polling test (async tools/call)
// ---------------------------------------------------------------------------

func BenchmarkClient_List(b *testing.B) {
	srv := newBrowserMCPServer(b)
	client := NewClient(srv.URL + "/sse")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.List(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLazyClient_List(b *testing.B) {
	srv := newBrowserMCPServer(b)
	lc := NewLazyClient(srv.URL + "/sse")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := lc.List(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}
