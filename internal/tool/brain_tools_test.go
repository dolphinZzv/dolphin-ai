package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

func setupTestBrainForTools(t *testing.T) *brain.Brain {
	t.Helper()
	dir := t.TempDir()
	b := brain.New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return b
}

func TestRegisterBrainTools(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"brain_read":  false,
		"brain_write": false,
		"brain_list":  false,
		"brain_log":   false,
	}

	for _, d := range defs {
		if _, ok := expected[d.Name]; ok {
			expected[d.Name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestBrainRead(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	// Write a file first.
	br.Write(context.Background(), "test.md", "", "hello world")

	args, _ := json.Marshal(map[string]string{"path": "test.md"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-1", Name: "brain_read", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Content)
	}
}

func TestBrainReadNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	args, _ := json.Marshal(map[string]string{"path": "nonexistent.md"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-2", Name: "brain_read", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing file")
	}
}

func TestBrainReadMissingPath(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	args, _ := json.Marshal(map[string]string{"path": ""})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "brain_read", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for empty path")
	}
}

func TestBrainReadInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "brain_read", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestBrainWrite(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	args, _ := json.Marshal(map[string]string{
		"path":    "newfile.md",
		"content": "new content",
		"summary": "test write",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-5", Name: "brain_write", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "written to brain") {
		t.Errorf("expected 'written to brain', got: %s", result.Content)
	}

	// Verify content was written.
	content, err := br.Read(context.Background(), "newfile.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "new content" {
		t.Errorf("expected 'new content', got %q", content)
	}
}

func TestBrainWriteMissingPath(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	args, _ := json.Marshal(map[string]string{"path": "", "content": "x"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-6", Name: "brain_write", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for empty path")
	}
}

func TestBrainWriteInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-7", Name: "brain_write", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestBrainList(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	// Write some files.
	br.Write(context.Background(), "a.md", "", "a")
	br.Write(context.Background(), "b.md", "", "b")

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-8", Name: "brain_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "a.md") {
		t.Errorf("expected a.md in list, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "b.md") {
		t.Errorf("expected b.md in list, got: %s", result.Content)
	}
}

func TestBrainListEmpty(t *testing.T) {
	// Use a brain with only seed files — list shows them.
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-9", Name: "brain_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// Should still contain seed files, not "(empty)"
	if !strings.Contains(result.Content, "introduction.md") {
		t.Errorf("expected seed files in list, got: %s", result.Content)
	}
}

func TestBrainLog(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	args, _ := json.Marshal(map[string]int{"n": 5})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "brain_log", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "init brain") {
		t.Errorf("expected 'init brain' in log, got: %s", result.Content)
	}
}

func TestBrainLogDefaultN(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterBrainTools(r, br)

	// Args with n=0 should default to 10.
	args, _ := json.Marshal(map[string]int{"n": 0})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-11", Name: "brain_log", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "init brain") {
		t.Errorf("expected 'init brain' in log, got: %s", result.Content)
	}
}
