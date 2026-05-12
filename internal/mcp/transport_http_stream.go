package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// httpStreamTransport communicates with a remote MCP server via Streamable HTTP.
type httpStreamTransport struct {
	cfg       config.MCPServerConfig
	name      string
	baseURL   string
	sessionID string
	client    *http.Client
}

func newHTTPStreamTransport(name string, cfg config.MCPServerConfig) (*httpStreamTransport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for http-stream transport", name)
	}
	return &httpStreamTransport{
		cfg:     cfg,
		name:    name,
		baseURL: strings.TrimRight(cfg.URL, "/"),
		client:  newHTTPClient(cfg.Timeout),
	}, nil
}

func (t *httpStreamTransport) connect(ctx context.Context) error {
	zap.S().Debugw("http-stream transport ready", "server", t.name, "url", t.baseURL)
	return nil
}

func (t *httpStreamTransport) sendRequest(ctx context.Context, reqJSON map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(reqJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/message", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	t.setHeaders(httpReq)
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}

	// Persist session ID if server sends one
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return t.parseSSEResponse(resp.Body, reqJSON["id"])
	}

	// Plain JSON response
	var msg struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("jsonrpc error: %s (code %d)", msg.Error.Message, msg.Error.Code)
	}
	return msg.Result, nil
}

// parseSSEResponse reads an SSE stream and extracts the JSON-RPC result.
func (t *httpStreamTransport) parseSSEResponse(r io.Reader, reqID any) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	var dataBuf []byte

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataBuf = append(dataBuf, []byte(data)...)
		} else if line == "" && len(dataBuf) > 0 {
			var msg struct {
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(dataBuf, &msg); err == nil {
				if msg.Error != nil {
					return nil, fmt.Errorf("jsonrpc error: %s (code %d)", msg.Error.Message, msg.Error.Code)
				}
				return msg.Result, nil
			}
			dataBuf = dataBuf[:0]
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read sse response: %w", err)
	}
	return nil, fmt.Errorf("no response event found")
}

func (t *httpStreamTransport) sendNotification(ctx context.Context, notif map[string]any) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/message", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	t.setHeaders(httpReq)
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (t *httpStreamTransport) close() error {
	return nil
}

func (t *httpStreamTransport) setHeaders(req *http.Request) {
	for k, v := range t.cfg.Headers {
		req.Header.Set(k, v)
	}
}
