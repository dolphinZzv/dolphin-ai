package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dolphin/internal/config"
)

func newTestACPConfig() *config.ACPConfig {
	return &config.ACPConfig{
		AgentID:      "test-agent",
		AgentName:    "Test Agent",
		AgentVersion: "0.1.0",
		AgentDesc:    "Test ACP agent",
		Capabilities: []string{"task-execution", "shell-command"},
		SyncTimeout:  "5s",
	}
}

// startTestACPTransport creates an ACPTransport with an httptest.Server for reliable port handling.
func startTestACPTransport(t *testing.T, cfg *config.ACPConfig) (*ACPTransport, string) {
	t.Helper()
	tr := NewACPTransport(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", tr.authMiddleware(tr.handleTasks))
	mux.HandleFunc("/tasks/", tr.authMiddleware(tr.handleTaskByID))
	mux.HandleFunc("/capabilities", tr.authMiddleware(tr.handleCapabilities))
	mux.HandleFunc("/agents/", tr.authMiddleware(tr.handleAgentCard))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return tr, srv.URL
}

func TestACPTransportName(t *testing.T) {
	tr := NewACPTransport(newTestACPConfig())
	if tr.Name() != "acp" {
		t.Errorf("Name() = %q, want acp", tr.Name())
	}
}

func TestACPTransportCapabilities(t *testing.T) {
	tr := NewACPTransport(newTestACPConfig())
	caps := tr.Capabilities()
	if caps.Streaming {
		t.Error("Streaming = true, want false")
	}
	if !caps.Flushable {
		t.Error("Flushable = false, want true")
	}
}

func TestACPTransportContext(t *testing.T) {
	tr := NewACPTransport(newTestACPConfig())
	ctx := tr.Context()
	if !strings.Contains(ctx, "ACP") {
		t.Error("Context() should mention ACP")
	}
	if !strings.Contains(ctx, "test-agent") {
		t.Error("Context() should contain agent ID")
	}
}

func TestACPTransportSyncTask(t *testing.T) {
	cfg := newTestACPConfig()
	tr, baseURL := startTestACPTransport(t, cfg)

	// Agent loop
	go func() {
		for {
			line, err := tr.ReadLine()
			if err != nil {
				return
			}
			tr.WriteLine("echo: " + line)
		}
	}()

	resp, err := http.Post(
		baseURL+"/tasks",
		"application/json",
		strings.NewReader(`{"task":"hello world"}`),
	)
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var trResp taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&trResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if trResp.Status != "completed" {
		t.Errorf("status = %q, want completed", trResp.Status)
	}
	if trResp.Output == nil {
		t.Fatal("output is nil")
	}
	if trResp.Output.Result != "echo: hello world" {
		t.Errorf("result = %q, want %q", trResp.Output.Result, "echo: hello world")
	}
}

func TestACPTransportAsyncTask(t *testing.T) {
	cfg := newTestACPConfig()
	tr, baseURL := startTestACPTransport(t, cfg)

	var responded atomic.Bool
	go func() {
		for {
			line, err := tr.ReadLine()
			if err != nil {
				return
			}
			responded.Store(true)
			tr.WriteLine("result: " + line)
		}
	}()

	// Async POST
	req, _ := http.NewRequest("POST", baseURL+"/tasks",
		strings.NewReader(`{"task":"async task"}`))
	req.Header.Set("Prefer", "respond-async")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, string(body))
	}

	var trResp taskResponse
	json.NewDecoder(resp.Body).Decode(&trResp)
	if trResp.Status != "pending" {
		t.Errorf("status = %q, want pending", trResp.Status)
	}
	taskID := trResp.ID
	if taskID == "" {
		t.Fatal("task ID is empty")
	}

	// Poll until completed
	var result taskResponse
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/tasks/" + taskID)
		if err != nil {
			t.Fatalf("GET /tasks/%s: %v", taskID, err)
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if result.Status == "completed" || result.Status == "failed" {
			break
		}
	}
	if result.Status != "completed" {
		t.Errorf("final status = %q, want completed", result.Status)
	}
	if !responded.Load() {
		t.Error("agent loop never responded")
	}
}

func TestACPTransportGetCapabilities(t *testing.T) {
	cfg := newTestACPConfig()
	_, baseURL := startTestACPTransport(t, cfg)

	resp, err := http.Get(baseURL + "/capabilities")
	if err != nil {
		t.Fatalf("GET /capabilities: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var caps map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&caps)

	if caps["agentId"] != "test-agent" {
		t.Errorf("agentId = %v, want test-agent", caps["agentId"])
	}
	if caps["protocol"] != "acp" {
		t.Errorf("protocol = %v, want acp", caps["protocol"])
	}
}

func TestACPTransportAuth(t *testing.T) {
	cfg := newTestACPConfig()
	cfg.APIKey = "test-secret-key"
	_, baseURL := startTestACPTransport(t, cfg)

	// Without auth
	resp, err := http.Get(baseURL + "/capabilities")
	if err != nil {
		t.Fatalf("GET /capabilities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// With auth
	req, _ := http.NewRequest("GET", baseURL+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer test-secret-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /capabilities with auth: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with auth, got %d", resp.StatusCode)
	}

	// Wrong key
	req, _ = http.NewRequest("GET", baseURL+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /capabilities wrong key: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", resp.StatusCode)
	}
}

func TestACPTransportCancelTask(t *testing.T) {
	cfg := newTestACPConfig()
	_, baseURL := startTestACPTransport(t, cfg)

	// Don't start agent loop — task stays pending

	// Use async POST to avoid blocking on sync timeout
	req, _ := http.NewRequest("POST", baseURL+"/tasks",
		strings.NewReader(`{"task":"cancellable task"}`))
	req.Header.Set("Prefer", "respond-async")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	var asyncResp taskResponse
	json.NewDecoder(resp.Body).Decode(&asyncResp)
	resp.Body.Close()

	taskID := asyncResp.ID
	if taskID == "" {
		t.Fatal("task ID is empty")
	}
	delReq, _ := http.NewRequest("DELETE", baseURL+"/tasks/"+taskID, nil)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /tasks/%s: %v", taskID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on cancel, got %d", resp.StatusCode)
	}

	// Verify cancelled
	resp, err = http.Get(baseURL + "/tasks/" + taskID)
	if err != nil {
		t.Fatalf("GET /tasks/%s: %v", taskID, err)
	}
	defer resp.Body.Close()

	var result taskResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", result.Status)
	}
}

func TestACPTransportAgentCard(t *testing.T) {
	cfg := newTestACPConfig()
	_, baseURL := startTestACPTransport(t, cfg)

	resp, err := http.Get(baseURL + "/agents/test-agent")
	if err != nil {
		t.Fatalf("GET /agents/test-agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var card map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&card)

	if card["agentId"] != "test-agent" {
		t.Errorf("agentId = %v, want test-agent", card["agentId"])
	}
}

func TestACPTransportInvalidMethod(t *testing.T) {
	cfg := newTestACPConfig()
	_, baseURL := startTestACPTransport(t, cfg)

	resp, err := http.Post(baseURL+"/capabilities", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /capabilities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestACPTransportConcurrentTasks(t *testing.T) {
	cfg := newTestACPConfig()
	tr, baseURL := startTestACPTransport(t, cfg)

	// Agent loop
	go func() {
		for {
			line, err := tr.ReadLine()
			if err != nil {
				return
			}
			tr.WriteString("processed: " + line)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"task":"task-%d"}`, i)
			resp, err := http.Post(
				baseURL+"/tasks",
				"application/json",
				strings.NewReader(payload),
			)
			if err != nil {
				t.Logf("request %d failed: %v", i, err)
				return
			}
			defer resp.Body.Close()

			var trResp taskResponse
			json.NewDecoder(resp.Body).Decode(&trResp)
			if trResp.Status != "completed" {
				t.Errorf("task %d status = %q, want completed", i, trResp.Status)
			}
		}(i)
	}
	wg.Wait()
}
