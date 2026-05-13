package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// WebhookTool sends HTTP webhook requests to configured or inline endpoints.
type WebhookTool struct {
	cfg    *config.MCPWebhookConfig
	schema json.RawMessage
	client *http.Client
}

// webhookInput is the JSON-unmarshal shape for the Execute input.
type webhookInput struct {
	Target  string            `json:"target"`  // named target from config
	URL     string            `json:"url"`     // inline URL (used when target is empty)
	Method  string            `json:"method"`  // HTTP method, default "POST"
	Headers map[string]string `json:"headers"` // extra headers, merged with target headers
	Body    string            `json:"body"`    // request body
}

func NewWebhookTool(cfg *config.Config) *WebhookTool {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Name of a pre-configured webhook target from config (mcp.webhook.targets). If set, url/method/headers may be omitted.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Webhook URL (required if no target specified).",
			},
			"method": map[string]any{
				"type":        "string",
				"description": "HTTP method, e.g. POST, GET, PUT. Default: POST.",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "Additional HTTP headers to send. Merged with target headers if a target is used.",
				"additionalProperties": map[string]any{
					"type": "string",
				},
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Request body content.",
			},
		},
	})
	return &WebhookTool{
		cfg:    &cfg.MCP.Webhook,
		schema: schema,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (w *WebhookTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "webhook",
		Description: "Send HTTP webhook requests to configured or arbitrary endpoints. Supports GET, POST, PUT, PATCH, DELETE. Use a named target from config or provide a direct URL.",
		InputSchema: w.schema,
		Priority:    w.cfg.Priority,
		Source:      "built-in",
	}
}

func (w *WebhookTool) Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error) {
	var params webhookInput
	if err := json.Unmarshal(input, &params); err != nil {
		return &ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	// Resolve URL
	url := params.URL
	method := params.Method
	headers := make(map[string]string)

	// Copy target headers first (if a named target is specified)
	if params.Target != "" {
		target, ok := w.cfg.Targets[params.Target]
		if !ok {
			return &ToolResult{Content: fmt.Sprintf("webhook target %q not found in config", params.Target), IsError: true}, nil
		}
		if url == "" {
			url = target.URL
		}
		if method == "" {
			method = target.Method
		}
		for k, v := range target.Headers {
			headers[k] = v
		}
	}

	if url == "" {
		return &ToolResult{Content: "no URL provided: set a target name or url parameter", IsError: true}, nil
	}
	if method == "" {
		method = "POST"
	}

	// Merge inline headers (override target headers)
	for k, v := range params.Headers {
		headers[k] = v
	}

	// Build request body
	var bodyReader io.Reader
	if params.Body != "" {
		bodyReader = bytes.NewBufferString(params.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("build request: %v", err), IsError: true}, nil
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && params.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	zap.S().Debugw("webhook: sending request", "method", method, "url", sanitizeURL(url))

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return &ToolResult{
		Content: fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, string(respBody)),
	}, nil
}


// sanitizeURL strips query params for logging to avoid leaking secrets in URLs.
func sanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "<invalid-url>"
	}
	if u.RawQuery != "" {
		u.RawQuery = "<redacted>"
	}
	if u.Fragment != "" {
		u.Fragment = "<redacted>"
	}
	return u.String()
}

// blockPrivateTarget checks if a URL points to a private or internal IP range
// to prevent SSRF attacks. Returns an error if the target is blocked.
func blockPrivateTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	host := u.Hostname()

	// Resolve hostname to IP addresses
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %v", host, err)
	}

	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		if isPrivateIP(parsed) {
			return fmt.Errorf("SSRF blocked: %q resolves to private IP %q", host, ip)
		}
	}
	return nil
}

// isPrivateIP checks if an IP falls in a private or link-local range.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	// Also check IPv4-mapped IPv6 for private ranges
	if ip4 := ip.To4(); ip4 != nil {
		return false // net.IP.IsPrivate() already covers 10/8, 172.16/12, 192.168/16, 127/8
	}
	// Block IPv6 unique-local (fc00::/7)
	return len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc
}
