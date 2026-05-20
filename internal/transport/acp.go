package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// --- Task model ---

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

	resultCh chan string `json:"-"` // sync mode: HTTP handler blocks here
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
	ID        string `json:"id"`
	AgentID   string `json:"agentId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Task      string `json:"task"`
	Context   string `json:"context,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type taskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output *taskOutput `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type taskOutput struct {
	Result      string `json:"result"`
	ContentType string `json:"contentType"`
}

// --- ACP Transport ---

// ACPTransport implements the IBM BeeAI Agent Communication Protocol as a Transport.
// It provides REST endpoints for other ACP-compatible agents to discover and invoke Dolphin.
type ACPTransport struct {
	cfg       *config.ACPConfig
	server    *http.Server
	msgCh     chan *acpTask
	closeCh   chan struct{}
	closeOnce sync.Once
	tasks     sync.Map // taskID -> *acpTask

	currentTask   *acpTask
	currentTaskMu sync.RWMutex
}

var _ Transport = (*ACPTransport)(nil)
var _ UserIO = (*ACPTransport)(nil)

func NewACPTransport(cfg *config.ACPConfig) *ACPTransport {
	return &ACPTransport{
		cfg:     cfg,
		msgCh:   make(chan *acpTask, 4096),
		closeCh: make(chan struct{}),
	}
}

func (t *ACPTransport) Name() string {
	return "acp"
}

func (t *ACPTransport) Context() string {
	return fmt.Sprintf("Connected via ACP (Agent Communication Protocol, IBM BeeAI). Listener: %s. Agent ID: %s.", t.cfg.ListenAddr, t.cfg.AgentID)
}

func (t *ACPTransport) Capabilities() Capabilities {
	return Capabilities{Streaming: false, Flushable: true}
}

// Start launches the ACP HTTP server and blocks until ctx is cancelled.
func (t *ACPTransport) Start(ctx context.Context) error {
	activeConnections.Add(1)
	defer activeConnections.Add(-1)

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

// ReadLine blocks until a task arrives via the HTTP server.
func (t *ACPTransport) ReadLine() (string, error) {
	select {
	case task, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("acp transport closed")
		}
		msgsReceived.Inc()

		t.currentTaskMu.Lock()
		t.currentTask = task
		t.currentTaskMu.Unlock()

		task.setRunning()
		return task.Input, nil
	case <-t.closeCh:
		return "", fmt.Errorf("acp transport closed")
	}
}

// WriteLine publishes a response to the current task's result channel.
func (t *ACPTransport) WriteLine(s string) error {
	return t.write(s)
}

// WriteString publishes a response to the current task's result channel.
func (t *ACPTransport) WriteString(s string) error {
	return t.write(s)
}

func (t *ACPTransport) write(s string) error {
	msgsSent.Inc()

	t.currentTaskMu.RLock()
	task := t.currentTask
	t.currentTaskMu.RUnlock()

	if task == nil {
		return fmt.Errorf("acp: no current task to respond to")
	}

	// Update task state (critical for async tasks where no handler waits on resultCh)
	task.setCompleted(s)

	// Try to notify sync handler; non-blocking in case it already timed out
	select {
	case task.resultCh <- s:
	default:
	}

	return nil
}

// Close shuts down the transport.
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

// --- Auth middleware ---

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

// --- HTTP handlers ---

// POST /tasks — submit a task
// GET /tasks — list active tasks (not in spec, convenience)
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

	// Check if caller wants async
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

	// Queue task for agent loop
	select {
	case t.msgCh <- task:
	case <-time.After(10 * time.Second):
		t.tasks.Delete(taskID)
		http.Error(w, `{"error":"server busy, task rejected"}`, http.StatusServiceUnavailable)
		return
	}

	if wantsAsync {
		// Return 202 with task ID immediately
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(taskResponse{
			ID:     taskID,
			Status: string(taskPending),
		})
		return
	}

	// Sync mode: wait for result
	select {
	case result := <-task.resultCh:
		task.setCompleted(result) // already set by write(), but idempotent
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

// GET /tasks/{id} — poll task status
// DELETE /tasks/{id} — cancel task
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

// GET /capabilities — list agent capabilities
func (t *ACPTransport) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]interface{}{
		"agentId":       t.cfg.AgentID,
		"name":          t.cfg.AgentName,
		"version":       t.cfg.AgentVersion,
		"description":   t.cfg.AgentDesc,
		"capabilities":  t.cfg.Capabilities,
		"protocol":      "acp",
		"protocolVersion": "0.1",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /agents/{id} — agent card (returns same as capabilities for self)
func (t *ACPTransport) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	t.handleCapabilities(w, r)
}

// --- helpers ---

func parseSyncTimeout(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 60 * time.Second
	}
	return d
}
