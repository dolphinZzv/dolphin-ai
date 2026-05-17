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
)

// sseResult is the JSON-RPC response structure from an SSE event.
type sseResult struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ParseSSEResult reads an SSE stream from r and returns the first JSON-RPC
// result object found. Events without a "data:" prefix are skipped.
func ParseSSEResult(r io.Reader) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	var dataBuf []byte

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			dataBuf = append(dataBuf, []byte(data)...)
		} else if line == "" && len(dataBuf) > 0 {
			var msg sseResult
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

// NewNotification creates an HTTP POST request for a JSON-RPC notification.
// The given doer executes the request and the response body is closed.
func NewNotification(ctx context.Context, url string, notif map[string]any, setHeaders func(*http.Request), doer interface {
	Do(*http.Request) (*http.Response, error)
}) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if setHeaders != nil {
		setHeaders(httpReq)
	}

	resp, err := doer.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	resp.Body.Close()
	return nil
}
