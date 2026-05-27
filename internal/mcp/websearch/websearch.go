package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"go.uber.org/zap"
)

// Tool provides web search capabilities.
type Tool struct {
	cfg         *config.MCPWebSearchConfig
	schema      json.RawMessage
	client      *http.Client
	defaultProv string // first in the configured ∩ registered intersection
}

// searchInput is the JSON-unmarshal shape for the Execute input.
type searchInput struct {
	Query    json.RawMessage `json:"query"`
	Provider string          `json:"provider,omitempty"`
}

// searchResult holds a single web search result.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchFunc is a provider search implementation.
type searchFunc func(w *Tool, ctx context.Context, query string) ([]searchResult, error)

var (
	providerNames []string
	searchFuncs   = map[string]searchFunc{}
)

// registerProvider registers a search provider name and its implementation.
// Called from init() in each provider file.
func registerProvider(name string, fn searchFunc) {
	providerNames = append(providerNames, name)
	searchFuncs[name] = fn
}

func New(cfg *config.Config) *Tool {
	// Determine configured providers (intersection of config ∩ registration)
	configured := cfg.MCP.WebSearch.Providers
	if len(configured) == 0 {
		// Backward compat: if single Provider is set, use that
		if cfg.MCP.WebSearch.Provider != "" {
			configured = []string{cfg.MCP.WebSearch.Provider}
		} else {
			configured = []string{"duckduckgo"}
		}
	}

	registered := make(map[string]bool, len(providerNames))
	for _, n := range providerNames {
		registered[n] = true
	}

	enum := make([]string, 0, len(configured))
	var defaultProv string
	for _, p := range configured {
		if registered[p] {
			enum = append(enum, p)
			if defaultProv == "" {
				defaultProv = p
			}
		}
	}

	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"oneOf": []map[string]any{
					{"type": "string", "description": "Search query"},
					{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Multiple search queries — each is searched independently and results are merged",
					},
				},
				"description": "Search query string or array of query strings. When multiple queries are provided, each is searched independently.",
			},
			"provider": map[string]any{
				"type":        "string",
				"enum":        enum,
				"description": "Search engine provider. Falls back to config default if not specified.",
			},
		},
		"required": []string{"query"},
	})
	return &Tool{
		cfg:         &cfg.MCP.WebSearch,
		schema:      schema,
		client:      &http.Client{Timeout: websearchTimeout(&cfg.MCP.WebSearch)},
		defaultProv: defaultProv,
	}
}

// OnConfigChange re-points the config sub-pointer and recreates the HTTP client.
func (w *Tool) OnConfigChange(oldCfg, newCfg *config.Config) {
	w.cfg = &newCfg.MCP.WebSearch
	w.client = &http.Client{Timeout: websearchTimeout(w.cfg)}
}

func (w *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for current information. Accepts a single query string or an array of queries for multi-angle research.",
		InputSchema: w.schema,
		Priority:    w.cfg.Priority,
		Source:      "built-in",
	}
}

func (w *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params searchInput
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	queries, err := parseQueries(params.Query)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid query: %v", err), IsError: true}, nil
	}
	if len(queries) == 0 {
		return &mcp.ToolResult{Content: "no query provided", IsError: true}, nil
	}

	provider := params.Provider
	if provider == "" {
		provider = w.defaultProv
	}

	fn, ok := searchFuncs[provider]
	if !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("unknown provider %q", provider), IsError: true}, nil
	}

	zap.S().Debugw("web_search: executing", "provider", provider, "queries", len(queries))

	var allResults []searchResult
	for _, q := range queries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		results, err := fn(w, ctx, q)
		if err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("search failed for %q: %v", q, err), IsError: true}, nil
		}
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		return &mcp.ToolResult{Content: "No results found."}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d result(s):\n\n", len(allResults))
	for i, r := range allResults {
		fmt.Fprintf(&sb, "%d. [%s](%s)\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.Snippet)
		}
		sb.WriteString("\n")
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

// parseQueries extracts one or more query strings from a JSON value
// that can be either a string or an array of strings.
func parseQueries(raw json.RawMessage) ([]string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}

	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("query must be a string or array of strings")
	}
	var out []string
	for _, q := range arr {
		if q != "" {
			out = append(out, q)
		}
	}
	return out, nil
}

func websearchTimeout(cfg *config.MCPWebSearchConfig) time.Duration {
	if cfg.TimeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(cfg.TimeoutSeconds) * time.Second
}
