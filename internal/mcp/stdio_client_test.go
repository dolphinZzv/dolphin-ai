package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"dolphin/internal/types"
)

func buildStdioTestServer(t testing.TB) string {
	t.Helper()
	src := `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dolphin/internal/mcp"
)

func main() {
	s := mcp.NewServer(mcp.ToolHandler{
		Name:        "echo",
		Description: "Echo the input back",
		Schema:      json.RawMessage(` + "`" + `{"type":"object","properties":{"text":{"type":"string"}}}` + "`" + `),
		Handle: func(ctx context.Context, args json.RawMessage) (any, error) {
			return map[string]string{"result": string(args)}, nil
		},
	}, mcp.ToolHandler{
		Name:        "ping",
		Description: "Returns pong",
		Schema:      json.RawMessage(` + "`" + `{}` + "`" + `),
		Handle: func(ctx context.Context, args json.RawMessage) (any, error) {
			return "pong", nil
		},
	})
	if err := s.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "serve error: %v\n", err)
		os.Exit(1)
	}
}
`

	wd, _ := os.Getwd()
	projectRoot := filepath.Join(wd, "..", "..")

	buildDir := filepath.Join(projectRoot, ".mcp-test-build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mainPath := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(buildDir, "test-server")

	t.Cleanup(func() { _ = os.RemoveAll(buildDir) })

	cmd := exec.Command("go", "build", "-o", binaryPath, buildDir)
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build test server failed: %v\nprojectRoot=%s\n%s", err, projectRoot, out)
	}
	return binaryPath
}

func TestNewStdioClient(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if client.cmd == nil {
		t.Fatal("expected cmd to be set")
	}
	if client.sc == nil {
		t.Fatal("expected scanner to be set")
	}
}

func TestNewStdioClient_BinaryNotFound(t *testing.T) {
	_, err := NewStdioClient(context.Background(), "/nonexistent/binary", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestStdioClient_List(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	if defs[0].Name != "echo" {
		t.Fatalf("expected 'echo', got '%s'", defs[0].Name)
	}
	if defs[1].Name != "ping" {
		t.Fatalf("expected 'ping', got '%s'", defs[1].Name)
	}
}

func TestStdioClient_Execute(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "echo",
		Arguments: `{"text":"hello"}`,
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

func TestStdioClient_ExecuteWithoutArgs(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

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

func TestStdioClient_ExecuteToolNotFound(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:   "call-3",
		Name: "nonexistent_tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for tool not found")
	}
}

func TestStdioClient_ListThenExecute(t *testing.T) {
	binaryPath := buildStdioTestServer(t)
	client, err := NewStdioClient(context.Background(), binaryPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	defs, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) == 0 {
		t.Fatal("expected at least one tool")
	}

	result, err := client.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "echo",
		Arguments: `{"text":"world"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatal("unexpected error")
	}
}
