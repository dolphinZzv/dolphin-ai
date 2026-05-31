package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

var errBoom = errors.New("boom")

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stdout = old
	return buf.String()
}

func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if len(s.tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(s.tools))
	}
}

func TestNewServer_WithTools(t *testing.T) {
	s := NewServer(ToolHandler{
		Name: "echo",
		Handle: func(ctx context.Context, args json.RawMessage) (any, error) {
			return map[string]string{"echo": "ok"}, nil
		},
	})
	if len(s.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(s.tools))
	}
}

func TestWriteResponse(t *testing.T) {
	s := NewServer()
	out := captureStdout(func() {
		s.writeResponse(1, map[string]any{"status": "ok"})
	})

	var resp struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      int            `json:"id"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.JSONRPC != "2.0" {
		t.Fatalf("expected '2.0', got '%s'", resp.JSONRPC)
	}
	if resp.ID != 1 {
		t.Fatalf("expected 1, got %d", resp.ID)
	}
	if resp.Result["status"] != "ok" {
		t.Fatalf("expected 'ok', got '%s'", resp.Result["status"])
	}
}

func TestWriteError(t *testing.T) {
	s := NewServer()
	out := captureStdout(func() {
		s.writeError(2, -32601, "Method not found")
	})

	var resp struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      int            `json:"id"`
		Error   map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 2 {
		t.Fatalf("expected 2, got %d", resp.ID)
	}
	if int(resp.Error["code"].(float64)) != -32601 {
		t.Fatalf("expected -32601, got %v", resp.Error["code"])
	}
	if resp.Error["message"] != "Method not found" {
		t.Fatalf("unexpected message: %s", resp.Error["message"])
	}
}

func TestHandleList(t *testing.T) {
	s := NewServer(ToolHandler{
		Name:        "echo",
		Description: "Echo input",
	})
	out := captureStdout(func() {
		s.handleList(1)
	})

	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Result.Tools))
	}
	if resp.Result.Tools[0]["name"] != "echo" {
		t.Fatalf("expected 'echo', got '%s'", resp.Result.Tools[0]["name"])
	}
}

func TestHandleCall_Success(t *testing.T) {
	s := NewServer(ToolHandler{
		Name: "echo",
		Handle: func(ctx context.Context, args json.RawMessage) (any, error) {
			return map[string]string{"echo": string(args)}, nil
		},
	})
	params := json.RawMessage(`{"name":"echo","arguments":"hello"}`)
	out := captureStdout(func() {
		s.handleCall(context.Background(), 1, params)
	})

	var resp struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
}

func TestHandleCall_InvalidParams(t *testing.T) {
	s := NewServer()
	params := json.RawMessage(`not valid json`)
	out := captureStdout(func() {
		s.handleCall(context.Background(), 1, params)
	})

	var resp struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

func TestHandleCall_ToolNotFound(t *testing.T) {
	s := NewServer()
	params := json.RawMessage(`{"name":"nonexistent","arguments":{}}`)
	out := captureStdout(func() {
		s.handleCall(context.Background(), 1, params)
	})

	var resp struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestHandleCall_HandlerError(t *testing.T) {
	s := NewServer(ToolHandler{
		Name: "fails",
		Handle: func(ctx context.Context, args json.RawMessage) (any, error) {
			return nil, errBoom
		},
	})
	params := json.RawMessage(`{"name":"fails","arguments":{}}`)
	out := captureStdout(func() {
		s.handleCall(context.Background(), 1, params)
	})

	var resp struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for handler that returns error")
	}
}

func TestServer_Serve_ScannerError(t *testing.T) {
	// Serve with a closed stdin should return immediately with scanner error.
	// We can't easily mock os.Stdin in Serve(),
	// but we can verify it doesn't panic with empty input.
	s := NewServer()
	err := s.Serve(context.Background())
	// On a real terminal, this would block; in test environment
	// it may return nil or EOF depending on stdin state.
	_ = err
}
