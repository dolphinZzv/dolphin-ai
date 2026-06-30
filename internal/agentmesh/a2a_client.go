package agentmesh

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

	"github.com/rs/xid"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

// methodToProto maps each A2A JSON-RPC method to the protocol version that
// introduced it. Used for version negotiation. cancel/get are proto=1 (basic
// task lifecycle) per the design doc's fix — a parent must always be able to
// terminate a runaway task.
var methodToProto = map[string]int{
	"tasks/send":          1,
	"tasks/cancel":        1,
	"tasks/get":           1,
	"agents/discover":     2,
	"agents/ping":         2,
	"tasks/sendSubscribe": 3,
	"tools/list":          4,
	"tools/call":          4,
}

// minSupportedProto is the lowest protocol version this client will talk to.
const minSupportedProto = 1

// localProto is the protocol version this build implements. Now that
// tasks/sendSubscribe, tasks/cancel, tasks/get, tools/list, tools/call are
// all implemented, we advertise proto=4.
const localProto = 4

// A2AClient is an HTTP JSON-RPC client to a peer agent's A2A server.
type A2AClient struct {
	addr   string // "host:port"
	http   *http.Client
	logger *zap.Logger
	tls    bool // whether to use https

	mu                sync.RWMutex
	card              AgentCard // discovered card
	negotiatedProto   int
	unsupported       map[string]bool
	negotiated        bool
}

// NewA2AClient builds a client for the given address (host:port).
func NewA2AClient(addr string, logger *zap.Logger) *A2AClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &A2AClient{
		addr:   addr,
		http:   &http.Client{Timeout: 11 * time.Minute},
		logger: logger,
		unsupported: map[string]bool{},
		negotiatedProto: localProto,
	}
}

// NewA2AClientWithTLS builds a client that uses mTLS for the connection.
// caCertPath is the CA that signed the peer's cert; clientCert/clientKey are
// this agent's client certificate. If insecureSkipVerify is true, the peer's
// cert is not validated (testing only).
func NewA2AClientWithTLS(addr string, tls *TLSConfig, logger *zap.Logger) (*A2AClient, error) {
	c := NewA2AClient(addr, logger)
	if tls == nil {
		return c, nil
	}
	tlsCfg, err := tls.Build()
	if err != nil {
		return nil, err
	}
	c.http = &http.Client{Timeout: 11 * time.Minute, Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	// rewrite scheme to https for call URLs
	c.tls = true
	return c, nil
}

// Negotiate discovers the peer's AgentCard and computes the negotiated proto.
// Safe to call multiple times; subsequent calls are no-ops once negotiated.
func (c *A2AClient) Negotiate(ctx context.Context) error {
	c.mu.RLock()
	if c.negotiated {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	card, err := c.Discover(ctx)
	if err != nil {
		return fmt.Errorf("negotiate: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.card = card
	peer := card.ProtoVersion
	if peer < minSupportedProto {
		return fmt.Errorf("agent %s proto=%d too old, min=%d", c.addr, peer, minSupportedProto)
	}
	neg := min(localProto, peer)
	c.negotiatedProto = neg
	c.unsupported = map[string]bool{}
	for method, required := range methodToProto {
		if required > neg {
			c.unsupported[method] = true
		}
	}
	c.negotiated = true
	c.logger.Info("agentmesh: negotiated",
		zap.String("addr", c.addr),
		zap.Int("proto", neg),
		zap.Int("peer_proto", peer),
	)
	return nil
}

// supports reports whether the peer supports the given method after negotiation.
// Before negotiation it optimistically returns true.
func (c *A2AClient) supports(method string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.unsupported[method]
}

// Card returns the discovered AgentCard (zero value before Negotiate).
func (c *A2AClient) Card() AgentCard {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.card
}

// NegotiatedProto returns the negotiated protocol version (localProto before
// Negotiate is called).
func (c *A2AClient) NegotiatedProto() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.negotiatedProto
}

// Discover calls agents/discover and returns the peer's AgentCard.
func (c *A2AClient) Discover(ctx context.Context) (AgentCard, error) {
	var card AgentCard
	if err := c.call(ctx, "agents/discover", nil, &card); err != nil {
		return AgentCard{}, err
	}
	if card.Addr == "" {
		card.Addr = c.addr
	}
	return card, nil
}

// Ping calls agents/ping for a lightweight health check.
func (c *A2AClient) Ping(ctx context.Context) error {
	var resp map[string]any
	return c.call(ctx, "agents/ping", nil, &resp)
}

// SendTask calls tasks/send with a DelegatePayload and returns the result.
//
// The peer's A2A server expects standard taskSendParams
// ({message:{parts:[{text}]}}), so we wrap the payload's Task (+ a context
// summary) into the message text. The routing fields (PreferredAgent,
// RequiredCapabilities) are consumed client-side by the Router and do not
// need to travel on the wire.
func (c *A2AClient) SendTask(ctx context.Context, payload DelegatePayload) (*DelegateResult, error) {
	if !c.supports("tasks/send") {
		return nil, fmt.Errorf("peer %s does not support tasks/send", c.addr)
	}
	text := payload.Task
	if payload.SystemPrompt != "" {
		text = payload.SystemPrompt + "\n\n" + text
	}
	for _, m := range payload.Context.Messages {
		text += "\n\n[context/" + m.Role + "]\n" + m.Content
	}
	for _, f := range payload.Context.Files {
		if f.Mode == FileInline && f.Content != "" {
			text += "\n\n[file: " + f.Path + "]\n" + f.Content
		} else {
			text += "\n\n[file: " + f.Path + " (reference)]"
		}
	}
	params := map[string]any{
		"id":        payload.ChildSessionID,
		"sessionId": payload.ParentSessionID,
		"message": map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"text": text}},
		},
	}
	// The server returns an a2aTaskResult {id, status, artifacts:[{parts:[{text}]}]}.
	// Decode it and lift the artifact text into DelegateResult.Content.
	var raw struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Artifacts []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"artifacts"`
	}
	if err := c.call(ctx, "tasks/send", params, &raw); err != nil {
		return nil, err
	}
	result := &DelegateResult{
		TaskID: raw.ID,
		Status: DelegateCompleted,
	}
	if raw.Status != "" && raw.Status != "completed" {
		result.Status = DelegateFailed
	}
	for _, a := range raw.Artifacts {
		for _, p := range a.Parts {
			result.Content += p.Text
		}
	}
	return result, nil
}

// CancelTask calls tasks/cancel.
func (c *A2AClient) CancelTask(ctx context.Context, taskID string) error {
	if !c.supports("tasks/cancel") {
		return fmt.Errorf("peer %s does not support tasks/cancel", c.addr)
	}
	return c.call(ctx, "tasks/cancel", map[string]any{"task_id": taskID}, nil)
}

// GetTask calls tasks/get to poll an async task's status.
func (c *A2AClient) GetTask(ctx context.Context, taskID string) (*DelegateResult, error) {
	if !c.supports("tasks/get") {
		return nil, fmt.Errorf("peer %s does not support tasks/get", c.addr)
	}
	var result DelegateResult
	if err := c.call(ctx, "tasks/get", map[string]any{"task_id": taskID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTools calls tools/list (proto=4) to discover the peer's exposed tools.
func (c *A2AClient) ListTools(ctx context.Context) ([]RemoteToolDef, error) {
	if !c.supports("tools/list") {
		return nil, fmt.Errorf("peer %s does not support tools/list (proto<4)", c.addr)
	}
	var tools []RemoteToolDef
	if err := c.call(ctx, "tools/list", nil, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// CallTool calls tools/call (proto=4) to invoke a peer's tool.
func (c *A2AClient) CallTool(ctx context.Context, name, arguments string) (*RemoteToolResult, error) {
	if !c.supports("tools/call") {
		return nil, fmt.Errorf("peer %s does not support tools/call (proto<4)", c.addr)
	}
	var result RemoteToolResult
	params := map[string]any{"name": name, "arguments": json.RawMessage(arguments)}
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RemoteToolDef is the wire shape returned by tools/list. Mirrors types.ToolDef
// but lives in agentmesh to avoid importing types here.
type RemoteToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// RemoteToolResult is the wire shape returned by tools/call.
type RemoteToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// SendTaskSubscribe calls tasks/sendSubscribe and streams SSE events. It
// returns a channel of StreamEvent; the channel closes when the stream ends.
// The final DelegateResult is delivered as the last event with Type="done"
// and also returned as the channel's terminal value via the returned result
// pointer (non-nil once the stream completes).
//
// Phase 2: the peer may downgrade to a single "done" event if it executes
// synchronously (current A2A transport model). Callers should not assume
// intermediate events arrive.
func (c *A2AClient) SendTaskSubscribe(ctx context.Context, payload DelegatePayload) (<-chan StreamEvent, error) {
	if !c.supports("tasks/sendSubscribe") {
		return nil, fmt.Errorf("peer %s does not support tasks/sendSubscribe", c.addr)
	}
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      xid.New().String(),
		Method:  "tasks/sendSubscribe",
		Params:  payload,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	scheme := "http"
	if c.tls {
		scheme = "https"
	}
	url := scheme + "://" + c.addr + "/jsonrpc"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	injectTraceContext(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	// Non-SSE fallback: if the server replied with JSON (synchronous
	// downgrade), decode a single DelegateResult and emit one done event.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var rpcResp rpcResponse
		if err := json.Unmarshal(raw, &rpcResp); err != nil {
			return nil, fmt.Errorf("unmarshal non-SSE response: %w", err)
		}
		if rpcResp.Error != nil {
			return nil, rpcResp.Error
		}
		var result DelegateResult
		_ = json.Unmarshal(rpcResp.Result, &result)
		ch := make(chan StreamEvent, 1)
		ch <- StreamEvent{Type: "done", Content: result.Content, Time: time.Now()}
		close(ch)
		return ch, nil
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		scanSSE(resp.Body, ch)
	}()
	return ch, nil
}

// scanSSE parses a text/event-stream body into StreamEvent values.
func scanSSE(r io.Reader, ch chan<- StreamEvent) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if payload, ok := strings.CutPrefix(line, "data: "); ok {
				var ev StreamEvent
				if json.Unmarshal([]byte(payload), &ev) == nil {
					if ev.Time.IsZero() {
						ev.Time = time.Now()
					}
					ch <- ev
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// --- low-level JSON-RPC ---

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message)
}

// call performs a single JSON-RPC method call against the peer.
func (c *A2AClient) call(ctx context.Context, method string, params, out any) error {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      xid.New().String(),
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	scheme := "http"
	if c.tls {
		scheme = "https"
	}
	url := scheme + "://" + c.addr + "/jsonrpc"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Inject W3C TraceContext so the peer's A2A server can continue the span.
	injectTraceContext(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal response: %w (body=%q)", err, truncate(string(raw), 200))
	}
	if rpcResp.Error != nil {
		// Try to unwrap a DelegateError from data.
		if len(rpcResp.Error.Data) > 0 {
			var dErr DelegateError
			if json.Unmarshal(rpcResp.Error.Data, &dErr) == nil && dErr.Code != "" {
				return &dErr
			}
		}
		return rpcResp.Error
	}
	if out != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, out); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
