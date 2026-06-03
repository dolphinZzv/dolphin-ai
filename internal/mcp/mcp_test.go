package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/h2non/gock"

	"dolphin/internal/types"
)

// gockClient creates a Client and registers it with gock for HTTP mocking.
func gockClient(baseURL string) *Client {
	client := NewClient(baseURL)
	gock.InterceptClient(client.http)
	return client
}

func TestClient_List(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
		}).
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"tools": []map[string]any{
					{
						"name":        "greet",
						"description": "Say hello",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{"type": "string"},
							},
						},
					},
					{
						"name":        "echo",
						"description": "Echo input",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			},
		})

	client := gockClient("http://mcp.example.com")
	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	if defs[0].Name != "greet" {
		t.Fatalf("expected 'greet', got '%s'", defs[0].Name)
	}
	if defs[1].Name != "echo" {
		t.Fatalf("expected 'echo', got '%s'", defs[1].Name)
	}
}

func TestClient_ListRPCError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32601,
				"message": "Method not found",
			},
		})

	client := gockClient("http://mcp.example.com")
	_, err := client.List(context.Background())
	if err == nil {
		t.Fatal("expected RPC error")
	}
}

func TestClient_ListHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		Reply(500)

	client := gockClient("http://mcp.example.com")
	_, err := client.List(context.Background())
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestClient_Execute(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "greet",
				"arguments": map[string]any{
					"name": "world",
				},
			},
		}).
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"content": "Hello, world!",
			},
		})

	client := gockClient("http://mcp.example.com")
	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "greet",
		Arguments: `{"name":"world"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
	if result.ToolCallID != "call-1" {
		t.Fatalf("expected 'call-1', got '%s'", result.ToolCallID)
	}
}

func TestClient_ExecuteWithoutArgs(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "ping",
				"arguments": nil,
			},
		}).
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "pong",
		})

	client := gockClient("http://mcp.example.com")
	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-2",
		Name: "ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
}

func TestClient_ExecuteRPCError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		Reply(200).
		JSON(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32000,
				"message": "Tool not found",
			},
		})

	client := gockClient("http://mcp.example.com")
	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-3",
		Name: "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for RPC error")
	}
}

func TestClient_ExecuteHTTPError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		Reply(http.StatusBadRequest)

	client := gockClient("http://mcp.example.com")
	_, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-4",
		Name: "test",
	})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestClient_ExecuteNetworkError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		ReplyError(NewFakeNetError("connection refused"))

	client := gockClient("http://mcp.example.com")
	_, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-5",
		Name: "test",
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestClient_JSONMarshalError(t *testing.T) {
	// Verify that the call function correctly handles a marshal error.
	// Since json.Marshal can't fail on the struct types we use, this is a
	// structural coverage test.
	client := gockClient("http://mcp.example.com")

	// Use an invalid base URL to trigger a request creation error.
	client.baseURL = "://invalid-url"
	_, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-6",
		Name: "test",
	})
	if err == nil {
		t.Fatal("expected error with invalid URL")
	}
}

func TestClient_ListUnmarshalError(t *testing.T) {
	defer gock.Off()

	gock.New("http://mcp.example.com").
		Post("/").
		Reply(200).
		BodyString("not json at all")

	client := gockClient("http://mcp.example.com")
	_, err := client.List(context.Background())
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

// NewFakeNetError returns an error that satisfies the net.Error interface.
func NewFakeNetError(msg string) error {
	return &fakeNetError{msg: msg}
}

type fakeNetError struct {
	msg string
}

func (e *fakeNetError) Error() string   { return e.msg }
func (e *fakeNetError) Timeout() bool   { return false }
func (e *fakeNetError) Temporary() bool { return false }

// Ensure json.RawMessage is used in the package.
var _ = json.RawMessage{}
