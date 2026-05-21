// Package a2a provides A2A (Agent-to-Agent) JSON-RPC transport.
// Implements Google's Agent-to-Agent protocol specification.
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func init() { transport.Register("a2a", New) }

// A2A task states.
type taskState string

const (
	taskSubmitted     taskState = "submitted"
	taskWorking       taskState = "working"
	taskInputRequired taskState = "input_required"
	taskAuthRequired  taskState = "auth_required"
	taskCompleted     taskState = "completed"
	taskFailed        taskState = "failed"
	taskCanceled      taskState = "canceled"
	taskRejected      taskState = "rejected"
)

type a2aTask struct {
	mu        sync.Mutex
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId,omitempty"`
	Input     string    `json:"input"`
	State     taskState `json:"state"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Created   time.Time `json:"created"`
	Completed time.Time `json:"completed,omitempty"`

	resultCh chan string `json:"-"`
}

func (t *a2aTask) setCompleted(output string) {
	t.mu.Lock()
	t.State = taskCompleted
	t.Output = output
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *a2aTask) setFailed(errMsg string) {
	t.mu.Lock()
	t.State = taskFailed
	t.Error = errMsg
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *a2aTask) setCanceled(errMsg string) {
	t.mu.Lock()
	t.State = taskCanceled
	t.Error = errMsg
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *a2aTask) setWorking() {
	t.mu.Lock()
	t.State = taskWorking
	t.mu.Unlock()
}

// ---- JSON-RPC types ----

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- A2A message/part types ----

type taskParams struct {
	ID      string      `json:"id"`
	Message *a2aMessage `json:"message,omitempty"`
}

type a2aMessage struct {
	Role  string    `json:"role"`
	Parts []a2aPart `json:"parts"`
}

type a2aPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ---- A2A response types ----

type taskStatus struct {
	State   string      `json:"state"`
	Message *a2aMessage `json:"message,omitempty"`
}

type taskResult struct {
	ID     string     `json:"id"`
	Status taskStatus `json:"status"`
}

type agentCard struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	URL                string                 `json:"url"`
	Version            string                 `json:"version"`
	ProtocolVersion    string                 `json:"protocolVersion"`
	Capabilities       map[string]bool        `json:"capabilities"`
	SecuritySchemes    map[string]interface{} `json:"securitySchemes"`
	Security           []string               `json:"security"`
	DefaultInputModes  []string               `json:"defaultInputModes"`
	DefaultOutputModes []string               `json:"defaultOutputModes"`
}

// A2ATransport implements the Google Agent-to-Agent (A2A) protocol as a Transport.
// Uses JSON-RPC 2.0 over HTTP on a single endpoint.
type A2ATransport struct {
	cfg       *config.A2AConfig
	server    *http.Server
	msgCh     chan *a2aTask
	closeCh   chan struct{}
	closeOnce sync.Once
	tasks     sync.Map

	currentTask   *a2aTask
	currentTaskMu sync.RWMutex
}

func New(cfg *config.Config) (transport.Transport, error) {
	return &A2ATransport{
		cfg:     &cfg.Transport.A2A,
		msgCh:   make(chan *a2aTask, 4096),
		closeCh: make(chan struct{}),
	}, nil
}

func (t *A2ATransport) Name() string { return "a2a" }

func (t *A2ATransport) Context() string {
	return fmt.Sprintf("Connected via A2A (Agent-to-Agent, Google). Listener: %s. Agent ID: %s.", t.cfg.ListenAddr, t.cfg.AgentID)
}

func (t *A2ATransport) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: false}
}

func (t *A2ATransport) Start(ctx context.Context) error {
	transport.ActiveConnections.Add(1)
	defer transport.ActiveConnections.Add(-1)

	mux := http.NewServeMux()
	mux.HandleFunc("/a2a", t.authMiddleware(t.handleRPC))
	mux.HandleFunc("/.well-known/agent.json", t.authMiddleware(t.handleAgentCard))

	t.server = &http.Server{
		Addr:              t.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		var err error
		if t.cfg.TLSEnabled && t.cfg.TLSCertFile != "" && t.cfg.TLSKeyFile != "" {
			err = t.server.ListenAndServeTLS(t.cfg.TLSCertFile, t.cfg.TLSKeyFile)
		} else {
			err = t.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			zap.S().Errorw("a2a http server error", "error", err)
		}
	}()

	zap.S().Infow("a2a transport started",
		"listen_addr", t.cfg.ListenAddr,
		"agent_id", t.cfg.AgentID,
		"tls", t.cfg.TLSEnabled,
	)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t.server.Shutdown(shutdownCtx)
	return t.Close()
}

func (t *A2ATransport) ReadLine() (string, error) {
	select {
	case task, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("a2a transport closed")
		}
		transport.MsgsReceived.Inc()

		t.currentTaskMu.Lock()
		t.currentTask = task
		t.currentTaskMu.Unlock()

		task.setWorking()
		return task.Input, nil
	case <-t.closeCh:
		return "", fmt.Errorf("a2a transport closed")
	}
}

func (t *A2ATransport) WriteLine(s string) error {
	return t.write(s)
}

func (t *A2ATransport) WriteString(s string) error {
	return t.write(s)
}

func (t *A2ATransport) Flush() error { return nil }

func (t *A2ATransport) write(s string) error {
	transport.MsgsSent.Inc()

	t.currentTaskMu.RLock()
	task := t.currentTask
	t.currentTaskMu.RUnlock()

	if task == nil {
		return fmt.Errorf("a2a: no current task to respond to")
	}

	task.setCompleted(s)

	select {
	case task.resultCh <- s:
	default:
	}

	return nil
}

func (t *A2ATransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closeCh)
		if t.server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			t.server.Shutdown(shutdownCtx)
		}
	})
	return nil
}

// ---- HTTP handlers ----

func (t *A2ATransport) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	if t.cfg.APIKey == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
			return
		}
		if strings.TrimPrefix(auth, "Bearer ") != t.cfg.APIKey {
			http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (t *A2ATransport) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, -32600, "Invalid Request: jsonrpc must be 2.0")
		return
	}

	switch req.Method {
	case "tasks/send":
		t.handleTasksSend(w, r, req)
	case "tasks/get":
		t.handleTasksGet(w, req)
	case "tasks/cancel":
		t.handleTasksCancel(w, req)
	default:
		writeRPCError(w, req.ID, -32601, "Method not found")
	}
}

func (t *A2ATransport) handleTasksSend(w http.ResponseWriter, r *http.Request, rpcReq jsonRPCRequest) {
	var params taskParams
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
		writeRPCError(w, rpcReq.ID, -32602, "Invalid params")
		return
	}

	taskID := params.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// Extract text input from the message parts
	input := ""
	if params.Message != nil {
		for _, p := range params.Message.Parts {
			if p.Type == "text" && p.Text != "" {
				input = p.Text
				break
			}
		}
	}
	if input == "" {
		writeRPCError(w, rpcReq.ID, -32602, "Invalid params: no text part in message")
		return
	}

	task := &a2aTask{
		ID:       taskID,
		Input:    input,
		State:    taskSubmitted,
		Created:  time.Now(),
		resultCh: make(chan string, 1),
	}

	t.tasks.Store(taskID, task)

	select {
	case t.msgCh <- task:
	case <-time.After(10 * time.Second):
		t.tasks.Delete(taskID)
		writeRPCError(w, rpcReq.ID, -32000, "Server busy, task rejected")
		return
	}

	// Wait for result synchronously
	select {
	case result := <-task.resultCh:
		task.setCompleted(result)
		writeRPCResult(w, rpcReq.ID, taskResult{
			ID: taskID,
			Status: taskStatus{
				State: string(taskCompleted),
				Message: &a2aMessage{
					Role: "agent",
					Parts: []a2aPart{
						{Type: "text", Text: result},
					},
				},
			},
		})

	case <-time.After(parseSyncTimeout(t.cfg.SyncTimeout)):
		task.setFailed("sync timeout")
		writeRPCResult(w, rpcReq.ID, taskResult{
			ID: taskID,
			Status: taskStatus{
				State: string(taskFailed),
			},
		})

	case <-r.Context().Done():
		task.setCanceled("client disconnected")
	}
}

func (t *A2ATransport) handleTasksGet(w http.ResponseWriter, rpcReq jsonRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil || params.ID == "" {
		writeRPCError(w, rpcReq.ID, -32602, "Invalid params: id required")
		return
	}

	val, ok := t.tasks.Load(params.ID)
	if !ok {
		writeRPCError(w, rpcReq.ID, -32001, "Task not found")
		return
	}
	task := val.(*a2aTask)

	task.mu.Lock()
	state := task.State
	output := task.Output
	task.mu.Unlock()

	var msg *a2aMessage
	if state == taskCompleted && output != "" {
		msg = &a2aMessage{
			Role: "agent",
			Parts: []a2aPart{
				{Type: "text", Text: output},
			},
		}
	}

	writeRPCResult(w, rpcReq.ID, taskResult{
		ID: params.ID,
		Status: taskStatus{
			State:   string(state),
			Message: msg,
		},
	})
}

func (t *A2ATransport) handleTasksCancel(w http.ResponseWriter, rpcReq jsonRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rpcReq.Params, &params); err != nil || params.ID == "" {
		writeRPCError(w, rpcReq.ID, -32602, "Invalid params: id required")
		return
	}

	val, ok := t.tasks.Load(params.ID)
	if !ok {
		writeRPCError(w, rpcReq.ID, -32001, "Task not found")
		return
	}
	task := val.(*a2aTask)
	task.setCanceled("canceled by caller")

	writeRPCResult(w, rpcReq.ID, taskResult{
		ID: params.ID,
		Status: taskStatus{
			State: string(taskCanceled),
		},
	})
}

func (t *A2ATransport) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	card := agentCard{
		Name:            t.cfg.AgentName,
		Description:     t.cfg.AgentDesc,
		URL:             fmt.Sprintf("http://localhost%s/a2a", t.cfg.ListenAddr),
		Version:         t.cfg.AgentVersion,
		ProtocolVersion: "1.0",
		Capabilities: map[string]bool{
			"streaming":              false,
			"pushNotifications":      false,
			"stateTransitionHistory": false,
		},
		SecuritySchemes:    map[string]interface{}{},
		Security:           []string{},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}

	if t.cfg.APIKey != "" {
		card.SecuritySchemes["apiKey"] = map[string]string{
			"type": "apiKey",
			"name": "Authorization",
			"in":   "header",
		}
		card.Security = []string{"apiKey"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(card)
}

// ---- helpers ----

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	var idVal interface{} = nil
	if len(id) > 0 && string(id) != "null" {
		json.Unmarshal(id, &idVal)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      idVal,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	var idVal interface{} = nil
	if len(id) > 0 && string(id) != "null" {
		json.Unmarshal(id, &idVal)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      idVal,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func parseSyncTimeout(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 60 * time.Second
	}
	return d
}
