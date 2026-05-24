package webhook

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
	"dolphin/internal/mcp"

	"go.uber.org/zap"
)

// Tool sends HTTP webhook requests to configured or inline endpoints.
type Tool struct {
	cfg         *config.MCPWebhookConfig
	schema      json.RawMessage
	client      *http.Client
	disableSSRF bool // set true in tests to bypass private IP check
}

// webhookInput is the JSON-unmarshal shape for the Execute input.
type webhookInput struct {
	Target  string            `json:"target"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func New(cfg *config.Config) *Tool {
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
	return &Tool{
		cfg:    &cfg.MCP.Webhook,
		schema: schema,
		client: &http.Client{Timeout: webhookTimeout(&cfg.MCP.Webhook)},
	}
}

func (w *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "webhook",
		Description: "Send HTTP webhook requests to configured or arbitrary endpoints. Supports GET, POST, PUT, PATCH, DELETE. Use a named target from config or provide a direct URL.",
		InputSchema: w.schema,
		Priority:    w.cfg.Priority,
		Source:      "built-in",
	}
}

func (w *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params webhookInput
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	urlStr := params.URL
	method := params.Method
	headers := make(map[string]string)

	if params.Target != "" {
		target, ok := w.cfg.Targets[params.Target]
		if !ok {
			return &mcp.ToolResult{Content: fmt.Sprintf("webhook target %q not found in config", params.Target), IsError: true}, nil
		}
		if urlStr == "" {
			urlStr = target.URL
		}
		if method == "" {
			method = target.Method
		}
		for k, v := range target.Headers {
			headers[k] = v
		}
	}

	if urlStr == "" {
		return &mcp.ToolResult{Content: "no URL provided: set a target name or url parameter", IsError: true}, nil
	}
	if method == "" {
		method = "POST"
	}

	for k, v := range params.Headers {
		headers[k] = v
	}

	var bodyReader io.Reader
	if params.Body != "" {
		bodyReader = bytes.NewBufferString(params.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("build request: %v", err), IsError: true}, nil
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && params.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// SSRF protection: reject requests to private/internal IPs
	if !w.disableSSRF {
		if err := blockPrivateTarget(urlStr); err != nil {
			return &mcp.ToolResult{Content: err.Error(), IsError: true}, nil
		}
	}

	zap.S().Debugw("webhook: sending request", "method", method, "url", sanitizeURL(urlStr))

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return &mcp.ToolResult{
		Content: fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, string(respBody)),
	}, nil
}

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

func blockPrivateTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	host := u.Hostname()

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

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return false
	}
	return len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc
}

func webhookTimeout(cfg *config.MCPWebhookConfig) time.Duration {
	if cfg.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(cfg.TimeoutSeconds) * time.Second
}
