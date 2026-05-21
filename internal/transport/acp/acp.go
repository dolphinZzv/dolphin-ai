// Package acp provides ACP (Agent Communication Protocol) REST transport.
package acp

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

func init() { transport.Register("acp", New) }

type taskStatus string

const (
	taskPending   taskStatus = "pending"
	taskRunning   taskStatus = "running"
	taskCompleted taskStatus = "completed"
	taskFailed    taskStatus = "failed"
	taskCancelled taskStatus = "cancelled"
)

type acpTask struct {
	mu        sync.Mutex
	ID        string     `json:"id"`
	AgentID   string     `json:"agentId,omitempty"`
	SessionID string     `json:"sessionId,omitempty"`
	Input     string     `json:"input"`
	Status    taskStatus `json:"status"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	Created   time.Time  `json:"created"`
	Completed time.Time  `json:"completed,omitempty"`

	resultCh chan string `json:"-"`
}

func (t *acpTask) setCompleted(output string) {
	t.mu.Lock()
	t.Status = taskCompleted
	t.Output = output
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *acpTask) setFailed(errMsg string) {
	t.mu.Lock()
	t.Status = taskFailed
	t.Error = errMsg
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *acpTask) setCancelled(errMsg string) {
	t.mu.Lock()
	t.Status = taskCancelled
	t.Error = errMsg
	t.Completed = time.Now()
	t.mu.Unlock()
}

func (t *acpTask) setRunning() {
	t.mu.Lock()
	t.Status = taskRunning
	t.mu.Unlock()
}

func (t *acpTask) snapshot() taskResponse {
	t.mu.Lock()
	defer t.mu.Unlock()
	resp := taskResponse{
		ID:     t.ID,
		Status: string(t.Status),
		Metadata: map[string]string{
			"created":   t.Created.Format(time.RFC3339),
			"completed": t.Completed.Format(time.RFC3339),
		},
	}
	if t.Status == taskCompleted {
		resp.Output = &taskOutput{Result: t.Output, ContentType: "text/plain"}
	} else if t.Status == taskFailed || t.Status == taskCancelled {
		resp.Error = t.Error
	}
	return resp
}

type taskRequest struct {
	ID        string            `json:"id"`
	AgentID   string            `json:"agentId,omitempty"`
	SessionID string            `json:"sessionId,omitempty"`
	Task      string            `json:"task"`
	Context   string            `json:"context,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type taskResponse struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Output   *taskOutput       `json:"output,omitempty"`
	Error    string            `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type taskOutput struct {
	Result      string `json:"result"`
	ContentType string `json:"contentType"`
}

// ACPTransport implements the IBM BeeAI Agent Communication Protocol as a Transport.
type ACPTransport struct {
	cfg       *config.ACPConfig
	server    *http.Server
	msgCh     chan *acpTask
	closeCh   chan struct{}
	closeOnce sync.Once
	tasks     sync.Map

	currentTask   *acpTask
	currentTaskMu sync.RWMutex
}

func New(cfg *config.Config) (transport.Transport, error) {
	return &ACPTransport{
		cfg:     &cfg.Transport.ACP,
		msgCh:   make(chan *acpTask, 4096),
		closeCh: make(chan struct{}),
	}, nil
}

func (t *ACPTransport) Name() string { return "acp" }

func (t *ACPTransport) Context() string {
	return fmt.Sprintf("Connected via ACP (Agent Communication Protocol, IBM BeeAI). Listener: %s. Agent ID: %s.", t.cfg.ListenAddr, t.cfg.AgentID)
}

func (t *ACPTransport) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: false}
}

func (t *ACPTransport) Start(ctx context.Context) error {
	transport.ActiveConnections.Add(1)
	defer transport.ActiveConnections.Add(-1)

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", t.authMiddleware(t.handleTasks))
	mux.HandleFunc("/tasks/", t.authMiddleware(t.handleTaskByID))
	mux.HandleFunc("/capabilities", t.authMiddleware(t.handleCapabilities))
	mux.HandleFunc("/agents/", t.authMiddleware(t.handleAgentCard))

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
			zap.S().Errorw("acp http server error", "error", err)
		}
	}()

	zap.S().Infow("acp transport started",
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

func (t *ACPTransport) ReadLine() (string, error) {
	select {
	case task, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("acp transport closed")
		}
		transport.MsgsReceived.Inc()

		t.currentTaskMu.Lock()
		t.currentTask = task
		t.currentTaskMu.Unlock()

		task.setRunning()
		return task.Input, nil
	case <-t.closeCh:
		return "", fmt.Errorf("acp transport closed")
	}
}

func (t *ACPTransport) WriteLine(s string) error {
	return t.write(s)
}

func (t *ACPTransport) WriteString(s string) error {
	return t.write(s)
}

func (t *ACPTransport) Flush() error { return nil }

func (t *ACPTransport) write(s string) error {
	transport.MsgsSent.Inc()

	t.currentTaskMu.RLock()
	task := t.currentTask
	t.currentTaskMu.RUnlock()

	if task == nil {
		return fmt.Errorf("acp: no current task to respond to")
	}

	task.setCompleted(s)

	select {
	case task.resultCh <- s:
	default:
	}

	return nil
}

func (t *ACPTransport) Close() error {
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

func (t *ACPTransport) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
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

func (t *ACPTransport) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t.handleCreateTask(w, r)
	case http.MethodGet:
		t.handleListTasks(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (t *ACPTransport) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request: %s"}`, err), http.StatusBadRequest)
		return
	}

	taskID := req.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	syncTimeout := parseSyncTimeout(t.cfg.SyncTimeout)
	wantsAsync := r.Header.Get("Prefer") == "respond-async"

	task := &acpTask{
		ID:        taskID,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
		Input:     req.Task,
		Status:    taskPending,
		Created:   time.Now(),
		resultCh:  make(chan string, 1),
	}

	t.tasks.Store(taskID, task)

	select {
	case t.msgCh <- task:
	case <-time.After(10 * time.Second):
		t.tasks.Delete(taskID)
		http.Error(w, `{"error":"server busy, task rejected"}`, http.StatusServiceUnavailable)
		return
	}

	if wantsAsync {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(taskResponse{
			ID:     taskID,
			Status: string(taskPending),
		})
		return
	}

	select {
	case result := <-task.resultCh:
		task.setCompleted(result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task.snapshot())

	case <-time.After(syncTimeout):
		task.setFailed("sync timeout")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(task.snapshot())

	case <-r.Context().Done():
		task.setCancelled("client disconnected")
	}
}

func (t *ACPTransport) handleListTasks(w http.ResponseWriter, r *http.Request) {
	var tasks []taskResponse
	t.tasks.Range(func(key, value interface{}) bool {
		at := value.(*acpTask)
		tasks = append(tasks, taskResponse{
			ID:     at.ID,
			Status: string(at.Status),
		})
		return true
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (t *ACPTransport) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if taskID == "" {
		http.Error(w, `{"error":"task id required"}`, http.StatusBadRequest)
		return
	}

	val, ok := t.tasks.Load(taskID)
	if !ok {
		http.Error(w, fmt.Sprintf(`{"error":"task %s not found"}`, taskID), http.StatusNotFound)
		return
	}
	task := val.(*acpTask)

	switch r.Method {
	case http.MethodGet:
		resp := task.snapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodDelete:
		task.setCancelled("cancelled by caller")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(task.snapshot())

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (t *ACPTransport) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]interface{}{
		"agentId":         t.cfg.AgentID,
		"name":            t.cfg.AgentName,
		"version":         t.cfg.AgentVersion,
		"description":     t.cfg.AgentDesc,
		"capabilities":    t.cfg.Capabilities,
		"protocol":        "acp",
		"protocolVersion": "0.1",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (t *ACPTransport) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	t.handleCapabilities(w, r)
}

func parseSyncTimeout(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 60 * time.Second
	}
	return d
}
