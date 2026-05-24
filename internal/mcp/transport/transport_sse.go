package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// sseTransport communicates with a remote MCP server via SSE (Server-Sent Events).
type sseTransport struct {
	cfg        config.MCPServerConfig
	name       string
	baseURL    string
	messageURL string
	client     *http.Client

	sseCancel context.CancelFunc
}

// NewSSE creates an SSE-based transport for a remote MCP server.
//
//nolint:revive
func NewSSE(name string, cfg config.MCPServerConfig) (*sseTransport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for sse transport", name)
	}
	return &sseTransport{
		cfg:     cfg,
		name:    name,
		baseURL: strings.TrimRight(cfg.URL, "/"),
		client:  NewHTTPClient(cfg.Timeout),
	}, nil
}

func (t *sseTransport) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/sse", http.NoBody)
	if err != nil {
		return fmt.Errorf("sse connect: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	t.setHeaders(req)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse connect: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("sse connect: HTTP %d", resp.StatusCode)
	}

	endpoint, err := t.readEndpoint(bufio.NewReader(resp.Body))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("sse connect: %w", err)
	}

	t.messageURL = t.baseURL + endpoint

	sseCtx, cancel := context.WithCancel(context.Background())
	t.sseCancel = cancel
	go t.listenSSE(sseCtx)

	zap.S().Debugw("sse transport connected", "server", t.name, "message_url", t.messageURL)
	return nil
}

func (t *sseTransport) readEndpoint(r *bufio.Reader) (string, error) {
	var eventType string
	var data string

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read sse event: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if line == "" {
			if eventType == "endpoint" && data != "" {
				return data, nil
			}
			eventType = ""
			data = ""
		}
	}
}

func (t *sseTransport) listenSSE(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/sse", http.NoBody)
		if err != nil {
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		t.setHeaders(req)

		resp, err := t.client.Do(req) //nolint:bodyclose
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(sseReconnectDelay(t.cfg.ReconnectDelay)):
				continue
			}
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var eventType string
		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data != "" {
					var notif struct {
						JSONRPC string `json:"jsonrpc"`
						Method  string `json:"method"`
					}
					if err := json.Unmarshal([]byte(data), &notif); err == nil && notif.Method != "" {
						if eventType != "" {
							zap.S().Infow("mcp server notification", "server", t.name, "event", eventType, "method", notif.Method)
						} else {
							zap.S().Infow("mcp server notification", "server", t.name, "method", notif.Method)
						}
					}
				}
				eventType = ""
			} else if line == "" {
				eventType = ""
			}
		}
		resp.Body.Close()
		if err := scanner.Err(); err != nil {
			zap.S().Debugw("sse connection closed, reconnecting", "server", t.name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sseReconnectDelay(t.cfg.ReconnectDelay)):
		}
	}
}

// sseReconnectDelay parses the configured reconnect delay string (e.g. "5s")
// and returns the duration. Returns 5s as default if unset or invalid.
func sseReconnectDelay(delay string) time.Duration {
	if delay == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(delay)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

func (t *sseTransport) SendRequest(ctx context.Context, reqJSON map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(reqJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	t.setHeaders(httpReq)

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return t.parseSSEResponse(resp.Body, reqJSON["id"])
	}

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

func (t *sseTransport) parseSSEResponse(r io.Reader, _ any) (json.RawMessage, error) {
	return ParseSSEResult(r)
}

func (t *sseTransport) SendNotification(ctx context.Context, notif map[string]any) error {
	return NewNotification(ctx, t.messageURL, notif, t.setHeaders, t.client)
}

func (t *sseTransport) Close() error {
	if t.sseCancel != nil {
		t.sseCancel()
	}
	return nil
}

func (t *sseTransport) setHeaders(req *http.Request) {
	for k, v := range t.cfg.Headers {
		req.Header.Set(k, v)
	}
}

// NewHTTPClient creates an http.Client with the configured timeout.
func NewHTTPClient(timeoutSec int) *http.Client {
	d := 30 * time.Second
	if timeoutSec > 0 {
		d = time.Duration(timeoutSec) * time.Second
	}
	return &http.Client{Timeout: d}
}
