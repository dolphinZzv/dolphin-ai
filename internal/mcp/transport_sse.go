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
	"sync"
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
	mu         sync.Mutex

	// SSE connection for server→client notifications
	sseCancel context.CancelFunc
}

func newSSETransport(name string, cfg config.MCPServerConfig) (*sseTransport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for sse transport", name)
	}
	return &sseTransport{
		cfg:     cfg,
		name:    name,
		baseURL: strings.TrimRight(cfg.URL, "/"),
		client:  newHTTPClient(cfg.Timeout),
	}, nil
}

func (t *sseTransport) connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/sse", nil)
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

	// Read the first event to get the message endpoint
	endpoint, err := t.readEndpoint(bufio.NewReader(resp.Body))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("sse connect: %w", err)
	}

	t.messageURL = t.baseURL + endpoint

	// Start background SSE listener for server→client notifications
	sseCtx, cancel := context.WithCancel(context.Background())
	t.sseCancel = cancel
	go t.listenSSE(sseCtx)

	zap.S().Debugw("sse transport connected", "server", t.name, "message_url", t.messageURL)
	return nil
}

// readEndpoint reads the first SSE event to extract the endpoint path.
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
			// Empty line = end of event
			if eventType == "endpoint" && data != "" {
				return data, nil
			}
			eventType = ""
			data = ""
		}
	}
}

// listenSSE maintains the SSE connection for server→client notifications.
// In the current implementation, server→client requests/notifications are not
// processed; this goroutine keeps the connection alive to avoid session expiry.
func (t *sseTransport) listenSSE(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/sse", nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		t.setHeaders(req)

		resp, err := t.client.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Drain the body until context is cancelled
		select {
		case <-ctx.Done():
			resp.Body.Close()
			return
		}
	}
}

func (t *sseTransport) sendRequest(ctx context.Context, reqJSON map[string]any) (json.RawMessage, error) {
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

// parseSSEResponse reads an SSE stream from the POST response body and extracts
// the JSON-RPC result matching the given request ID.
func (t *sseTransport) parseSSEResponse(r io.Reader, reqID any) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	var dataBuf []byte

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataBuf = append(dataBuf, []byte(data)...)
		} else if line == "" && len(dataBuf) > 0 {
			var msg struct {
				ID     json.RawMessage `json:"id"`
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

func (t *sseTransport) sendNotification(ctx context.Context, notif map[string]any) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	t.setHeaders(httpReq)

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (t *sseTransport) close() error {
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

// newHTTPClient creates an http.Client with the configured timeout.
func newHTTPClient(timeoutSec int) *http.Client {
	d := 30 * time.Second
	if timeoutSec > 0 {
		d = time.Duration(timeoutSec) * time.Second
	}
	return &http.Client{Timeout: d}
}
