package agentmesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockHandler is a function that handles a JSON-RPC method in MockA2AServer.
// Returning (nil, nil) yields an empty result; returning a non-nil error
// produces a JSON-RPC error response with the error's message.
type MockHandler func(method string, params json.RawMessage) (any, error)

// MockA2AServer is a test double for a peer agent's A2A server.
type MockA2AServer struct {
	mu       sync.Mutex
	handler  MockHandler
	srv      *httptest.Server
	calls    []string // recorded methods
	callsMu  sync.Mutex
}

// NewMockA2AServer starts an httptest server that dispatches JSON-RPC methods
// to the handler.
func NewMockA2AServer(handler MockHandler) *MockA2AServer {
	m := &MockA2AServer{handler: handler}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.callsMu.Lock()
		m.calls = append(m.calls, req.Method)
		m.callsMu.Unlock()

		result, err := m.handler(req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if err != nil {
			resp["error"] = map[string]any{"code": -32000, "message": err.Error()}
		} else {
			resp["result"] = result
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return m
}

// Addr returns the host:port of the mock server.
func (m *MockA2AServer) Addr() string { return m.srv.Listener.Addr().String() }

// URL returns the base URL of the mock server.
func (m *MockA2AServer) URL() string { return m.srv.URL }

// Calls returns the recorded method call sequence (test introspection).
func (m *MockA2AServer) Calls() []string {
	m.callsMu.Lock()
	defer m.callsMu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// Close shuts down the mock server.
func (m *MockA2AServer) Close() { m.srv.Close() }

// SetHandler swaps the handler (for mid-test behaviour changes).
func (m *MockA2AServer) SetHandler(h MockHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = h
}

// DefaultMockCard is a typical AgentCard returned by the mock discover handler.
func DefaultMockCard(name, addr string) AgentCard {
	return AgentCard{
		Name:         name,
		Addr:         addr,
		Capabilities: []string{"code-review"},
		Status:       AgentRunning,
		MaxLoad:      5,
		ProtoVersion: 2,
	}
}

// StaticHandler returns a MockHandler that serves discover/ping from a card
// and returns a fixed DelegateResult for tasks/send.
func StaticHandler(card AgentCard, result *DelegateResult) MockHandler {
	return func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "agents/discover":
			return card, nil
		case "agents/ping":
			return map[string]any{"status": "ok", "load": 0}, nil
		case "tasks/send":
			// Return the A2A task format that SendTask decodes:
			//   {id, status, artifacts:[{parts:[{text}]}]}
			var taskID, content string
			if result != nil {
				taskID = result.TaskID
				content = result.Content
			}
			if taskID == "" {
				taskID = "t-1"
			}
			if content == "" {
				content = "ok"
			}
			return map[string]any{
				"id":     taskID,
				"status": "completed",
				"artifacts": []map[string]any{{
					"parts": []map[string]any{{"text": content}},
				}},
			}, nil
		}
		return nil, nil
	}
}

// NewTestAgentMesh builds an AgentMesh wired to the given mock server, with
// the peer registered under the given name.
func NewTestAgentMesh(mock *MockA2AServer, name string, card AgentCard, opts ...func(*AgentConfig)) (*AgentMesh, func()) {
	cfg := DefaultAgentConfig()
	cfg.Enabled = true
	cfg.TaskTimeout = 0 // let tests control timeouts via context
	cfg.Retry.MaxRetries = 0
	cfg.CircuitBreaker.FailureThreshold = 3
	cfg.CircuitBreaker.CooldownPeriod = 1
	cfg.RateLimit.SendPerAgent = 1000
	cfg.RateLimit.SendBurst = 1000
	for _, o := range opts {
		o(&cfg)
	}
	mesh := NewAgentMesh(cfg, nil, nil)
	mesh.Register(card)
	return mesh, func() { _ = mesh.Shutdown() }
}
