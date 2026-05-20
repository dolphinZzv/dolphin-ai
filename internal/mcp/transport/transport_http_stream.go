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

	"dolphin/internal/config"
	"dolphin/internal/event"

	"go.uber.org/zap"
)

// httpStreamTransport communicates with a remote MCP server via Streamable HTTP.
type httpStreamTransport struct {
	cfg       config.MCPServerConfig
	name      string
	baseURL   string
	sessionID string
	client    *http.Client
	bus       *event.EventBus
}

// NewHTTPStream creates a Streamable HTTP transport for a remote MCP server.
//
//nolint:revive
func NewHTTPStream(name string, cfg config.MCPServerConfig, bus *event.EventBus) (*httpStreamTransport, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for http-stream transport", name)
	}
	return &httpStreamTransport{
		cfg:     cfg,
		name:    name,
		baseURL: strings.TrimRight(cfg.URL, "/"),
		client:  NewHTTPClient(cfg.Timeout),
			bus: bus,
	}, nil
}

func (t *httpStreamTransport) Connect(ctx context.Context) error {
	zap.S().Debugw("http-stream transport ready", "server", t.name, "url", t.baseURL)
	return nil
}

func (t *httpStreamTransport) SendRequest(ctx context.Context, reqJSON map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(reqJSON)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
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

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
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

func (t *httpStreamTransport) parseSSEResponse(r io.Reader, reqID any) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
				Method string          `json:"method"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(dataBuf, &msg); err != nil {
				dataBuf = dataBuf[:0]
				continue
			}

			// Server notification (no id, has method)
			if len(msg.ID) == 0 && msg.Method != "" {
				zap.S().Infow("mcp server notification", "server", t.name, "method", msg.Method)
				if t.bus != nil {
					t.bus.Emit(context.Background(), event.Event{
						Type: event.TypeMCPServerNotification,
						Data: map[string]any{
							"server": t.name,
							"method": msg.Method,
						},
					})
				}
				dataBuf = dataBuf[:0]
				continue
			}

			// Response for our request (has id)
			if len(msg.ID) > 0 {
				if msg.Error != nil {
					return nil, fmt.Errorf("jsonrpc error: %s (code %d)", msg.Error.Message, msg.Error.Code)
				}
				return msg.Result, nil
			}

			dataBuf = dataBuf[:0]
		}
	}
	return nil, fmt.Errorf("no response event found for id %v", reqID)
}

func (t *httpStreamTransport) SendNotification(ctx context.Context, notif map[string]any) error {
	url := t.baseURL
	setHeaders := func(req *http.Request) {
		t.setHeaders(req)
		if t.sessionID != "" {
			req.Header.Set("Mcp-Session-Id", t.sessionID)
		}
	}
	return NewNotification(ctx, url, notif, setHeaders, t.client)
}

func (t *httpStreamTransport) Close() error {
	return nil
}

func (t *httpStreamTransport) setHeaders(req *http.Request) {
	for k, v := range t.cfg.Headers {
		req.Header.Set(k, v)
	}
}
