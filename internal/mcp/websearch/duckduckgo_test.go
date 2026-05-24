package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dolphin/internal/config"
)

// ---- unescapeHTML ----

func TestUnescapeHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello &amp; world", "hello & world"},
		{"&lt;script&gt;", "<script>"},
		{"&gt;= 10", ">= 10"},
		{"&quot;quoted&quot;", `"quoted"`},
		{"it&#x27;s", "it's"},
		{"&#39;single&#39;", "'single'"},
		{"plain text", "plain text"},
		{"", ""},
	}
	for _, tc := range tests {
		got := unescapeHTML(tc.input)
		if got != tc.expected {
			t.Errorf("unescapeHTML(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ---- parseDuckDuckGoHTML ----

func TestParseDuckDuckGoHTML(t *testing.T) {
	html := `<html>
<body>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://example.com/go">Go Programming</a>
    <a class="result__snippet">Go is a compiled language.</a>
  </div>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://golang.org/pkg">Go Standard Library</a>
    <a class="result__snippet">Packages for fmt, http, json &amp; more.</a>
  </div>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://example.com/test">No Snippet</a>
  </div>
</body>
</html>`

	results := parseDuckDuckGoHTML(html, 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	checkResult(t, results[0], "Go Programming", "https://example.com/go", "Go is a compiled language.")
	checkResult(t, results[1], "Go Standard Library", "https://golang.org/pkg", "Packages for fmt, http, json & more.")
	checkResult(t, results[2], "No Snippet", "https://example.com/test", "")
}

func TestParseDuckDuckGoHTML_Empty(t *testing.T) {
	results := parseDuckDuckGoHTML("<html></html>", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseDuckDuckGoHTML_AmpersandURL(t *testing.T) {
	html := `<html><body>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://example.com/a?x=1&amp;y=2">Link with &amp;</a>
    <a class="result__snippet">Entity &amp; more.</a>
  </div>
</body></html>`

	results := parseDuckDuckGoHTML(html, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].URL != "https://example.com/a?x=1&y=2" {
		t.Fatalf("expected URL with unescaped &, got %q", results[0].URL)
	}
	if results[0].Title != "Link with &" {
		t.Fatalf("expected title 'Link with &', got %q", results[0].Title)
	}
}

func checkResult(t *testing.T, r searchResult, title, url, snippet string) {
	t.Helper()
	if r.Title != title {
		t.Errorf("expected title %q, got %q", title, r.Title)
	}
	if r.URL != url {
		t.Errorf("expected URL %q, got %q", url, r.URL)
	}
	if r.Snippet != snippet {
		t.Errorf("expected snippet %q, got %q", snippet, r.Snippet)
	}
}

// ---- Execute DuckDuckGo via HTTP mock ----

func TestExecuteDDG_SingleQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://go.dev/">The Go Programming Language</a>
    <a class="result__snippet">Go is an open source programming language.</a>
  </div>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://go.dev/doc/">Go Documentation</a>
    <a class="result__snippet">Official Go docs &amp; tutorials.</a>
  </div>
</body></html>`))
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		u := ts.URL + "?" + req.URL.RawQuery
		mockReq, _ := http.NewRequest(req.Method, u, nil)
		mockReq.Header = req.Header
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{"query": "golang"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "The Go Programming Language") {
		t.Fatalf("expected 'The Go Programming Language', got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://go.dev/") {
		t.Fatalf("expected https://go.dev/ in result, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Go Documentation") {
		t.Fatalf("expected 'Go Documentation', got: %s", result.Content)
	}
}

func TestExecuteDDG_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>No results.</body></html>"))
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mockReq, _ := http.NewRequest("GET", ts.URL, nil)
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{"query": "xyznonexistent"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "No results found." {
		t.Fatalf("expected 'No results found.', got: %s", result.Content)
	}
}

func TestExecuteDDG_MultipleQueries(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Fprint(w, `<html><body>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://example.com/r1">Result1</a>
    <a class="result__snippet">First result</a>
  </div></body></html>`)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mockReq, _ := http.NewRequest("GET", ts.URL+"?"+req.URL.RawQuery, nil)
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{
		"query": []string{"golang", "rust"},
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 search calls, got %d", callCount)
	}
	if !strings.Contains(result.Content, "2 result(s)") {
		t.Fatalf("expected 2 results in output, got: %s", result.Content)
	}
}

func TestExecuteDDG_FetchAndParse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>
  <div class="result">
    <a rel="nofollow" class="result__a" href="https://example.com">Example</a>
    <a class="result__snippet">Example domain.</a>
  </div>
</body></html>`))
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.WebSearch.Provider = "duckduckgo"
	tool := New(cfg)
	tool.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mockReq, _ := http.NewRequest("GET", ts.URL, nil)
		return http.DefaultTransport.RoundTrip(mockReq)
	})

	input, _ := json.Marshal(map[string]any{"query": "test"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Example") {
		t.Fatalf("expected 'Example' in result, got: %s", result.Content)
	}
}
