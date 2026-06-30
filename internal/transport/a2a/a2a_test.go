package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestA2A_NewA2A(t *testing.T) {
	a := NewA2A(A2AConfig{
		Addr:        ":0",
		Name:        "test-agent",
		Description: "A test agent",
		URL:         "http://localhost:8100",
		Version:     "1.0.0",
	}, nil)

	if a.ID() != "a2a" {
		t.Fatalf("expected 'a2a', got '%s'", a.ID())
	}
}

func TestA2A_Capability(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	c := a.Capability()
	if c.Interactive {
		t.Fatal("expected Interactive=false")
	}
	if c.Streamable {
		t.Fatal("expected Streamable=false")
	}
	if c.NestRead {
		t.Fatal("expected NestRead=false")
	}
	if c.RenderTextMarkdown != "markdown" {
		t.Fatalf("expected markdown render, got '%s'", c.RenderTextMarkdown)
	}
}

func TestA2A_RequestPermission(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	result, err := a.RequestPermission(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error for permission request")
	}
	if result != 0 {
		t.Fatal("expected PermissionDenied")
	}
}

func TestA2A_Context(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	ctx := a.Context()
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}
}

func TestA2A_Tools(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	tools := a.Tools()
	if tools != nil {
		t.Fatal("expected nil tools")
	}
}

func TestA2A_StartAndClose(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	ctx := context.Background()

	err := a.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	err = a.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestA2A_DoubleClose(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	ctx := context.Background()

	_ = a.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	_ = a.Close()
	err := a.Close()
	if err != nil {
		t.Fatalf("second close should be no-op: %v", err)
	}
}

func TestA2A_DoubleStart(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	ctx := context.Background()

	err := a.Start(ctx)
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	defer a.Close()

	err = a.Start(ctx)
	if err != nil {
		t.Fatalf("second start should be no-op: %v", err)
	}
}

func TestA2A_AgentCard(t *testing.T) {
	a := NewA2A(A2AConfig{
		Addr:        ":0",
		Name:        "test-agent",
		Description: "Test description",
		URL:         "http://example.com",
		Version:     "2.0.0",
	}, nil)

	ctx := context.Background()
	err := a.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer a.Close()

	time.Sleep(100 * time.Millisecond)

	// Find the actual port the server is listening on.
	// We need to extract it after starting with :0.
	addr := a.Addr()
	resp, err := http.Get(fmt.Sprintf("http://%s/.well-known/agent.json", addr))
	if err != nil {
		t.Fatalf("GET agent card failed: %v", err)
	}
	defer resp.Body.Close()

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if card.Name != "test-agent" {
		t.Fatalf("expected name 'test-agent', got '%s'", card.Name)
	}
	if card.Protocol != "a2a/1.0" {
		t.Fatalf("expected protocol 'a2a/1.0', got '%s'", card.Protocol)
	}
}

func TestA2A_TaskSendAndResponse(t *testing.T) {
	a := NewA2A(A2AConfig{
		Addr: ":0",
		Name: "test-agent",
	}, nil)

	ctx := context.Background()
	err := a.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer a.Close()

	time.Sleep(100 * time.Millisecond)
	addr := a.Addr()

	// Simulate what the agent loop does: read and write.
	go func() {
		msg, err := a.Read(ctx)
		if err != nil {
			return
		}
		if msg.Text != "Hello A2A" {
			t.Errorf("expected 'Hello A2A', got '%s'", msg.Text)
		}
		_ = a.Write(ctx, "Hello from agent")
	}()

	// Give the reader a moment to start waiting.
	time.Sleep(50 * time.Millisecond)

	// Send a JSON-RPC tasks/send request.
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"id":"task1","message":{"role":"user","parts":[{"text":"Hello A2A"}]}}}`
	resp, err := http.Post(
		fmt.Sprintf("http://%s/", addr),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp a2aResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if rpcResp.Error != nil {
		t.Fatalf("unexpected RPC error: %s", rpcResp.Error.Message)
	}
}

func TestA2A_JSONRPCErrors(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0", Name: "test"}, nil)

	ctx := context.Background()
	err := a.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer a.Close()

	time.Sleep(100 * time.Millisecond)
	addr := a.Addr()

	tests := []struct {
		name string
		body string
	}{
		{"GET request", ""}, // will be sent as GET
		{"invalid JSON", "not json"},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"invalid/method","params":{}}`},
		{"missing params", `{"jsonrpc":"2.0","id":1,"method":"tasks/send"}`},
		{"tasks/get unsupported", `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			var err error
			if tt.name == "GET request" {
				resp, err = http.Get(fmt.Sprintf("http://%s/", addr))
			} else {
				resp, err = http.Post(
					fmt.Sprintf("http://%s/", addr),
					"application/json",
					strings.NewReader(tt.body),
				)
			}
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			var rpcResp a2aResponse
			if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if rpcResp.Error == nil {
				t.Fatal("expected RPC error")
			}
		})
	}
}

func TestA2A_ReadClosed(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	_ = a.Close()

	_, err := a.Read(context.Background())
	if err == nil {
		t.Fatal("expected error reading from closed transport")
	}
}

func TestA2A_Flush(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	err := a.Flush()
	if err != nil {
		t.Fatalf("flush failed: %v", err)
	}
}

func TestA2A_WriteNoPendingResponse(t *testing.T) {
	a := NewA2A(A2AConfig{Addr: ":0"}, nil)
	// Write with no pending response should not error.
	err := a.Write(context.Background(), "test")
	if err != nil {
		t.Fatalf("write without pending response should not error: %v", err)
	}
}

func TestValOr(t *testing.T) {
	cfg := map[string]any{"key": "value", "empty": ""}

	if v := valOr(cfg, "key", "default"); v != "value" {
		t.Fatalf("expected 'value', got '%s'", v)
	}
	if v := valOr(cfg, "empty", "default"); v != "default" {
		t.Fatalf("expected 'default' for empty, got '%s'", v)
	}
	if v := valOr(cfg, "missing", "default"); v != "default" {
		t.Fatalf("expected 'default' for missing, got '%s'", v)
	}
}
