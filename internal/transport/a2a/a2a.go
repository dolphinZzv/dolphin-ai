package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
	"dolphin/internal/transport"
)

func init() {
	transport.Register("a2a", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
		logger, _ := cfg["logger"].(*zap.Logger)
		agentName, _ := cfg["agent_name"].(string)
		return NewA2A(A2AConfig{
			Addr:        valOr(cfg, "addr", ":8100"),
			Name:        valOr(cfg, "name", agentName),
			Description: valOr(cfg, "description", ""),
			URL:         valOr(cfg, "url", ""),
			Version:     valOr(cfg, "version", "1.0.0"),
		}, logger), nil
	})
}

func valOr(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

// A2AConfig holds the configuration for the A2A transport.
type A2AConfig struct {
	Addr        string // listen address, e.g. ":8100"
	Name        string // agent name
	Description string // agent description
	URL         string // agent URL (for agent card)
	Version     string // agent version
}

// A2A is an HTTP-based transport that implements the Google Agent-to-Agent protocol.
type A2A struct {
	*transport.SessionHolder
	cfg    A2AConfig
	logger *zap.Logger

	httpServer *http.Server
	listenAddr string // actual listen address (resolved when using :0)
	msgChan    chan *a2aRequest
	closeCh    chan struct{}

	mu       sync.Mutex
	closed   bool
	started  bool
	respChan chan string // pending response for current request
}

// a2aRequest carries an incoming A2A message and a channel for the response.
type a2aRequest struct {
	content string
	taskID  string
	respond chan string
}

func NewA2A(cfg A2AConfig, logger *zap.Logger) *A2A {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8100"
	}
	return &A2A{
		SessionHolder: transport.NewSessionHolder(nil),
		cfg:           cfg,
		logger:        logger,
		msgChan:       make(chan *a2aRequest, 100),
		closeCh:       make(chan struct{}),
	}
}

func (a *A2A) ID() string { return "a2a" }

func (a *A2A) Context() string {
	return i18n.T("a2a.context")
}

func (a *A2A) Tools() []common.ToolDesc { return nil }

func (a *A2A) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return nil
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("a2a: listen: %w", err)
	}
	a.listenAddr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent.json", a.handleAgentCard)
	mux.HandleFunc("/", a.handleJSONRPC)

	a.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.started = true

	go func() {
		a.logger.Info("a2a: starting HTTP server", zap.String("addr", a.listenAddr))
		if err := a.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			a.logger.Error("a2a: server error", zap.Error(err))
		}
	}()

	return nil
}

// Addr returns the actual listen address (useful when :0 is used to pick a random port).
func (a *A2A) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listenAddr
}

// Read blocks until an A2A message is received.
func (a *A2A) Read(ctx context.Context) (string, error) {
	select {
	case req := <-a.msgChan:
		a.mu.Lock()
		a.respChan = req.respond
		a.mu.Unlock()
		return req.content, nil
	case <-a.closeCh:
		return "", fmt.Errorf("a2a: closed")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Write sends the response to the pending A2A request.
func (a *A2A) Write(ctx context.Context, text string) error {
	a.mu.Lock()
	ch := a.respChan
	a.respChan = nil
	a.mu.Unlock()

	if ch == nil {
		a.logger.Warn("a2a: Write called with no pending response channel")
		return nil
	}

	select {
	case ch <- text:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return fmt.Errorf("a2a: response timeout")
	}
	return nil
}

func (a *A2A) Flush() error {
	a.mu.Lock()
	a.respChan = nil
	a.mu.Unlock()
	return nil
}

func (a *A2A) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	close(a.closeCh)
	a.mu.Unlock()

	if a.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.httpServer.Shutdown(ctx)
	}
	return nil
}

func (a *A2A) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        false,
		Streamable:         false,
		NestRead:           false,
		RenderTextMarkdown: "markdown",
	}
}

func (a *A2A) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return transport.PermissionDenied, fmt.Errorf("%s", i18n.T("a2a.no_interactive"))
}

func (a *A2A) Confirm(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("%s", i18n.T("a2a.no_interactive"))
}

// ---------------------------------------------------------------------------
// HTTP handlers (A2A protocol)
// ---------------------------------------------------------------------------

// AgentCard is the A2A agent card (/.well-known/agent.json).
type AgentCard struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Version     string `json:"version,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
}

func (a *A2A) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card := AgentCard{
		Name:        a.cfg.Name,
		Description: a.cfg.Description,
		URL:         a.cfg.URL,
		Version:     a.cfg.Version,
		Protocol:    "a2a/1.0",
	}
	w.Header().Set("Content-Type", "application/json")
	// Encode writes to the response; a failure means the client is gone,
	// nothing to recover.
	_ = json.NewEncoder(w).Encode(card)
}

// a2aRequestMsg is an incoming JSON-RPC request.
type a2aRequestMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// taskSendParams are the parameters for tasks/send.
type taskSendParams struct {
	ID        string     `json:"id"`
	SessionID string     `json:"sessionId,omitempty"`
	Message   a2aMessage `json:"message"`
	ContextID string     `json:"contextId,omitempty"`
}

// a2aMessage is a message within a task.
type a2aMessage struct {
	Role  string    `json:"role"`
	Parts []a2aPart `json:"parts"`
}

// a2aPart is a single part of a message.
type a2aPart struct {
	Text string `json:"text,omitempty"`
}

// a2aResponse is a JSON-RPC response.
type a2aResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *a2aError `json:"error,omitempty"`
}

type a2aError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// a2aTaskResult is the result returned by tasks/send or tasks/get.
type a2aTaskResult struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionId,omitempty"`
	ContextID string        `json:"contextId,omitempty"`
	Status    string        `json:"status"`
	Artifacts []a2aArtifact `json:"artifacts,omitempty"`
}

type a2aArtifact struct {
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	Parts       []a2aPart `json:"parts"`
}

func (a *A2A) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.writeJSONRPCError(w, nil, -32600, "only POST is accepted")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.writeJSONRPCError(w, nil, -32700, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req a2aRequestMsg
	if err := json.Unmarshal(body, &req); err != nil {
		a.writeJSONRPCError(w, nil, -32700, "invalid JSON-RPC request")
		return
	}

	switch req.Method {
	case "tasks/send":
		a.handleTaskSend(w, r, &req)
	case "tasks/get":
		a.handleTaskGet(w, &req)
	default:
		a.logger.Debug("a2a: unknown method", zap.String("method", req.Method))
		a.writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func (a *A2A) handleTaskSend(w http.ResponseWriter, r *http.Request, req *a2aRequestMsg) {
	var params taskSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		a.writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}

	// Extract text from the message parts.
	var content string
	for _, part := range params.Message.Parts {
		if part.Text != "" {
			content += part.Text + "\n"
		}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		a.writeJSONRPCError(w, req.ID, -32602, "message has no text content")
		return
	}

	taskID := params.ID
	if taskID == "" {
		taskID = xid.New().String()
	}

	a.logger.Info("a2a: received task",
		zap.String("task_id", taskID),
		zap.String("session_id", params.SessionID),
	)

	// Push to agent loop and wait for response.
	respCh := make(chan string, 1)
	select {
	case a.msgChan <- &a2aRequest{
		content: content,
		taskID:  taskID,
		respond: respCh,
	}:
	case <-a.closeCh:
		a.writeJSONRPCError(w, req.ID, -32000, "transport closed")
		return
	case <-time.After(5 * time.Second):
		a.writeJSONRPCError(w, req.ID, -32000, "transport busy")
		return
	}

	// Wait for the agent loop to produce a response.
	var responseText string
	select {
	case responseText = <-respCh:
	case <-r.Context().Done():
		return
	case <-time.After(5 * time.Minute):
		a.writeJSONRPCError(w, req.ID, -32000, "request timeout")
		return
	}

	result := a2aTaskResult{
		ID:        taskID,
		SessionID: params.SessionID,
		ContextID: params.ContextID,
		Status:    "completed",
		Artifacts: []a2aArtifact{
			{
				Name: "response",
				Parts: []a2aPart{
					{Text: responseText},
				},
			},
		},
	}

	a.writeJSONRPCResult(w, req.ID, result)
}

func (a *A2A) handleTaskGet(w http.ResponseWriter, req *a2aRequestMsg) {
	a.writeJSONRPCError(w, req.ID, -32601, "task polling not supported; use tasks/send for synchronous responses")
}

func (a *A2A) writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a2aResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (a *A2A) writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200
	_ = json.NewEncoder(w).Encode(a2aResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &a2aError{Code: code, Message: message},
	})
}

// Ensure A2A implements IO.
var _ transport.IO = (*A2A)(nil)
