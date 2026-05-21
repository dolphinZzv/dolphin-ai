package a2a

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"
)

func newTestA2AConfig() *config.A2AConfig {
	return &config.A2AConfig{
		AgentID:      "test-agent",
		AgentName:    "Test Agent",
		AgentVersion: "0.1.0",
		AgentDesc:    "Test A2A agent",
		Capabilities: []string{"task-execution", "shell-command"},
		SyncTimeout:  "5s",
	}
}

func startTestTransport(t *testing.T, cfg *config.A2AConfig) (*A2ATransport, string) {
	t.Helper()
	tr := &A2ATransport{
		cfg:     cfg,
		msgCh:   make(chan *a2aTask, 4096),
		closeCh: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/a2a", tr.authMiddleware(tr.handleRPC))
	mux.HandleFunc("/.well-known/agent.json", tr.authMiddleware(tr.handleAgentCard))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return tr, srv.URL
}

func TestName(t *testing.T) {
	tr := &A2ATransport{cfg: newTestA2AConfig()}
	if tr.Name() != "a2a" {
		t.Errorf("Name() = %q, want a2a", tr.Name())
	}
}

func TestCapabilities(t *testing.T) {
	cfg := newTestA2AConfig()
	tr := &A2ATransport{cfg: cfg}
	uio := transport.UserIO(tr)
	caps := uio.Capabilities()
	if caps.Streaming {
		t.Error("Streaming = true, want false")
	}
}

func TestContext(t *testing.T) {
	cfg := newTestA2AConfig()
	tr := &A2ATransport{cfg: cfg}
	ctx := tr.Context()
	if !strings.Contains(ctx, "A2A") {
		t.Error("Context() should mention A2A")
	}
	if !strings.Contains(ctx, "test-agent") {
		t.Error("Context() should contain agent ID")
	}
}

func TestSyncTask(t *testing.T) {
	cfg := newTestA2AConfig()
	tr, baseURL := startTestTransport(t, cfg)

	go func() {
		for {
			line, err := tr.ReadLine()
			if err != nil {
				return
			}
			tr.WriteLine("echo: " + line)
		}
	}()

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"test-1","message":{"role":"user","parts":[{"type":"text","text":"hello world"}]}}}`
	resp, err := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(rb))
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("RPC error: %+v", rpcResp.Error)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result taskResult
	json.Unmarshal(resultJSON, &result)

	if result.Status.State != "completed" {
		t.Errorf("state = %q, want completed", result.Status.State)
	}
	if result.Status.Message == nil || len(result.Status.Message.Parts) == 0 {
		t.Fatal("expected message with parts")
	}
	if result.Status.Message.Parts[0].Text != "echo: hello world" {
		t.Errorf("result text = %q, want %q", result.Status.Message.Parts[0].Text, "echo: hello world")
	}
}

func TestTaskGetAfterCompletion(t *testing.T) {
	cfg := newTestA2AConfig()
	tr, baseURL := startTestTransport(t, cfg)

	go func() {
		line, err := tr.ReadLine()
		if err != nil {
			return
		}
		tr.WriteLine("result for " + line)
	}()

	// Send task
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"task-123","message":{"role":"user","parts":[{"type":"text","text":"test"}]}}}`
	resp, _ := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	resp.Body.Close()

	// Get task status
	body = `{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"task-123"}}`
	resp, err := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a for get: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result taskResult
	json.Unmarshal(resultJSON, &result)

	if result.Status.State != "completed" {
		t.Errorf("state = %q, want completed", result.Status.State)
	}
}

func TestTaskCancel(t *testing.T) {
	cfg := newTestA2AConfig()
	tr, baseURL := startTestTransport(t, cfg)

	// Create a task without reading it (so it stays in pending)
	tr.tasks.Store("task-456", &a2aTask{
		ID:    "task-456",
		Input: "test",
		State: taskSubmitted,
	})

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/cancel","params":{"id":"task-456"}}`
	resp, err := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("RPC error: %+v", rpcResp.Error)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result taskResult
	json.Unmarshal(resultJSON, &result)

	if result.Status.State != "canceled" {
		t.Errorf("state = %q, want canceled", result.Status.State)
	}
}

func TestAgentCard(t *testing.T) {
	cfg := newTestA2AConfig()
	_, baseURL := startTestTransport(t, cfg)

	resp, err := http.Get(baseURL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET /.well-known/agent.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var card agentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if card.Name != "Test Agent" {
		t.Errorf("name = %q, want 'Test Agent'", card.Name)
	}
	if card.ProtocolVersion != "1.0" {
		t.Errorf("protocolVersion = %q, want '1.0'", card.ProtocolVersion)
	}
}

func TestAuthMiddleware(t *testing.T) {
	cfg := newTestA2AConfig()
	cfg.APIKey = "secret-key"
	_, baseURL := startTestTransport(t, cfg)

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"id":"any"}}`

	// Without auth header
	resp, _ := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// With wrong auth
	req, _ := http.NewRequest("POST", baseURL+"/a2a", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-key")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// With correct auth
	req, _ = http.NewRequest("POST", baseURL+"/a2a", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct key, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestJSONRPCParseError(t *testing.T) {
	cfg := newTestA2AConfig()
	_, baseURL := startTestTransport(t, cfg)

	resp, _ := http.Post(baseURL+"/a2a", "application/json", bytes.NewReader([]byte(`not json`)))
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error == nil || rpcResp.Error.Code != -32700 {
		t.Errorf("expected error -32700, got %+v", rpcResp.Error)
	}
}

func TestMethodNotFound(t *testing.T) {
	cfg := newTestA2AConfig()
	_, baseURL := startTestTransport(t, cfg)

	body := `{"jsonrpc":"2.0","id":1,"method":"bogus","params":{}}`
	resp, _ := http.Post(baseURL+"/a2a", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error == nil || rpcResp.Error.Code != -32601 {
		t.Errorf("expected error -32601, got %+v", rpcResp.Error)
	}
}

func TestWriteNoTask(t *testing.T) {
	cfg := newTestA2AConfig()
	tr := &A2ATransport{cfg: cfg}

	err := tr.write("test")
	if err == nil {
		t.Fatal("expected error when no current task")
	}
}
