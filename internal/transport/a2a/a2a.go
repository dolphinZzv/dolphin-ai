package a2a

import (
	"context"
	"encoding/json"
	"errors"
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

// ExtHandler is an extension JSON-RPC handler. Extensions are registered by
// AgentMesh (and other packages) to add methods beyond the core A2A set,
// without modifying a2a.go's switch. The handler receives the method name,
// raw params, and the HTTP request (for headers/trace context) and returns
// either a result value (JSON-encodable) or an error. Returning ErrExtUnhandled
// lets the dispatcher fall through to the core handlers.
type ExtHandler func(ctx context.Context, method string, params json.RawMessage, httpReq *http.Request) (any, error)

// ErrExtUnhandled signals that an extension handler does not claim this
// request; the dispatcher should try the next handler or the core switch.
var ErrExtUnhandled = fmt.Errorf("a2a: extension does not handle this method")

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

	extMu       sync.RWMutex
	extHandlers map[string]ExtHandler // method → handler (insertion-ordered via map; Phase 1 needs no ordering)
	sseHandlers map[string]SSEHandler // method → SSE streaming handler
	// self card fields surfaced for agents/discover
	selfCapabilities []string
	selfProtoVersion int
	selfLoad         func() int // current load reporter, optional
}

// SSEHandler handles a streaming JSON-RPC method by writing SSE events
// directly to the ResponseWriter. Registered by AgentMesh for
// tasks/sendSubscribe. The handler owns the response lifecycle: it must set
// Content-Type and flush per event.
type SSEHandler func(w http.ResponseWriter, r *http.Request, params json.RawMessage)

// RegisterExtHandler registers an extension JSON-RPC handler for a method.
// It is safe to call before Start. AgentMesh uses this to add agents/discover,
// agents/ping, tasks/cancel, tools/list, etc. without a2a depending on
// agentmesh (avoiding an import cycle).
func (a *A2A) RegisterExtHandler(method string, h ExtHandler) {
	a.extMu.Lock()
	defer a.extMu.Unlock()
	if a.extHandlers == nil {
		a.extHandlers = make(map[string]ExtHandler)
	}
	a.extHandlers[method] = h
}

// SetSelfCard configures the fields surfaced by agents/discover and agents/ping.
// capabilities and load are optional (load may be nil).
func (a *A2A) SetSelfCard(capabilities []string, protoVersion int, load func() int) {
	a.extMu.Lock()
	defer a.extMu.Unlock()
	a.selfCapabilities = capabilities
	a.selfProtoVersion = protoVersion
	a.selfLoad = load
}

// RegisterSSEHandler registers a streaming handler for a method. The handler
// writes SSE events directly to the ResponseWriter. AgentMesh uses this for
// tasks/sendSubscribe.
func (a *A2A) RegisterSSEHandler(method string, h SSEHandler) {
	a.extMu.Lock()
	defer a.extMu.Unlock()
	if a.sseHandlers == nil {
		a.sseHandlers = make(map[string]SSEHandler)
	}
	a.sseHandlers[method] = h
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
func (a *A2A) Read(ctx context.Context) (transport.Input, error) {
	select {
	case req := <-a.msgChan:
		a.mu.Lock()
		a.respChan = req.respond
		a.mu.Unlock()
		return transport.Input{Text: req.content}, nil
	case <-a.closeCh:
		return transport.Input{}, fmt.Errorf("a2a: closed")
	case <-ctx.Done():
		return transport.Input{}, ctx.Err()
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
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	URL          string   `json:"url"`
	Version      string   `json:"version,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	ProtoVersion int      `json:"proto_version,omitempty"`
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

	// 0. SSE streaming handlers (tasks/sendSubscribe). Checked first because
	//    the response is a stream, not a single JSON-RPC object.
	a.extMu.RLock()
	sse, hasSSE := a.sseHandlers[req.Method]
	a.extMu.RUnlock()
	if hasSSE {
		sse(w, r, req.Params)
		return
	}

	// 1. Extension handlers (AgentMesh registers discover/ping/cancel/...).
	//    When agents.enabled=false the map is empty and this is a no-op,
	//    preserving pre-upgrade behaviour.
	a.extMu.RLock()
	ext, ok := a.extHandlers[req.Method]
	a.extMu.RUnlock()
	if ok {
		result, err := ext(r.Context(), req.Method, req.Params, r)
		if err == nil {
			a.writeJSONRPCResult(w, req.ID, result)
			return
		}
		if !errors.Is(err, ErrExtUnhandled) {
			a.writeJSONRPCError(w, req.ID, -32000, err.Error())
			return
		}
		// fall through to core handlers
	}

	// 2. Core A2A methods.
	switch req.Method {
	case "tasks/send":
		a.handleTaskSend(w, r, &req)
	case "tasks/get":
		a.handleTaskGet(w, &req)
	case "agents/discover":
		a.handleAgentDiscover(w, &req)
	case "agents/ping":
		a.handleAgentPing(w, &req)
	case "tasks/sendSubscribe":
		// SSE streaming. Phase 2 downgrade: execute synchronously (same path
		// as tasks/send) then emit a single "done" event. True streaming
		// requires an async task model on the receiver, which is Phase 5+.
		a.handleTaskSendSubscribe(w, r, &req)
	default:
		a.logger.Debug("a2a: unknown method", zap.String("method", req.Method))
		a.writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

// handleAgentDiscover returns this agent's card. agents/discover is proto=2
// but is implemented in the core package so it works even when AgentMesh
// extension handlers are not wired (e.g. a standalone a2a server).
func (a *A2A) handleAgentDiscover(w http.ResponseWriter, req *a2aRequestMsg) {
	a.extMu.RLock()
	defer a.extMu.RUnlock()
	card := AgentCard{
		Name:         a.cfg.Name,
		Description:  a.cfg.Description,
		URL:          a.cfg.URL,
		Version:      a.cfg.Version,
		Protocol:     "a2a/1.0",
		Capabilities: a.selfCapabilities,
		ProtoVersion: a.selfProtoVersion,
	}
	a.writeJSONRPCResult(w, req.ID, card)
}

// handleAgentPing is a lightweight health check.
func (a *A2A) handleAgentPing(w http.ResponseWriter, req *a2aRequestMsg) {
	a.extMu.RLock()
	load := 0
	if a.selfLoad != nil {
		load = a.selfLoad()
	}
	a.extMu.RUnlock()
	a.writeJSONRPCResult(w, req.ID, map[string]any{"status": "ok", "load": load})
}

// handleTaskSendSubscribe is the SSE variant of tasks/send. Phase 2
// downgrade: it runs the task synchronously (reusing the tasks/send path)
// and emits a single "done" event with the final content. This keeps the
// protocol surface complete; true incremental streaming arrives with the
// async task model in a later phase.
func (a *A2A) handleTaskSendSubscribe(w http.ResponseWriter, r *http.Request, req *a2aRequestMsg) {
	var params taskSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		a.writeJSONRPCError(w, req.ID, -32602, "invalid params")
		return
	}
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

	// Push to agent loop and wait for the response synchronously.
	respCh := make(chan string, 1)
	select {
	case a.msgChan <- &a2aRequest{content: content, taskID: taskID, respond: respCh}:
	case <-a.closeCh:
		a.writeJSONRPCError(w, req.ID, -32000, "transport closed")
		return
	case <-time.After(5 * time.Second):
		a.writeJSONRPCError(w, req.ID, -32000, "transport busy")
		return
	}
	var responseText string
	select {
	case responseText = <-respCh:
	case <-r.Context().Done():
		return
	case <-time.After(5 * time.Minute):
		responseText = "[timeout]"
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	doneEvent := map[string]any{
		"type":    "done",
		"content": responseText,
		"status":  "completed",
		"task_id": taskID,
	}
	payload, _ := json.Marshal(doneEvent)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	if flusher != nil {
		flusher.Flush()
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
