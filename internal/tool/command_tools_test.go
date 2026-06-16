package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

func TestRegisterCommandTools(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"commands_list":   false,
		"command_upsert":  false,
		"command_toggle":  false,
		"command_execute": false,
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

func TestCommandsList(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name":        "test-cmd",
		"description": "a test command",
		"content":     "do something",
	})
	_, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-0", Name: "command_upsert", Arguments: string(createArgs),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-1", Name: "commands_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test-cmd") {
		t.Errorf("expected test-cmd in list, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "a test command") {
		t.Errorf("expected description in list, got: %s", result.Content)
	}
}

func TestCommandsListEmpty(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-2", Name: "commands_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "No commands found" {
		t.Errorf("expected 'No commands found', got: %s", result.Content)
	}
}

func TestCommandUpsertCreate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	args, _ := json.Marshal(map[string]string{
		"name":        "my-cmd",
		"description": "my command",
		"content":     "run this",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "command_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"my-cmd" created`) {
		t.Errorf("expected created message, got: %s", result.Content)
	}

	cmd, err := brain.ReadCommand(context.Background(), br, "my-cmd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Content, "run this") {
		t.Errorf("expected content containing 'run this', got %q", cmd.Content)
	}
}

func TestCommandUpsertUpdate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	// Create first.
	createArgs, _ := json.Marshal(map[string]string{
		"name":        "upsert-cmd",
		"description": "original",
		"content":     "original content",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "command_upsert", Arguments: string(createArgs),
	})

	// Same name = update.
	updateArgs, _ := json.Marshal(map[string]string{
		"name":    "upsert-cmd",
		"content": "updated content",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-5", Name: "command_upsert", Arguments: string(updateArgs),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"upsert-cmd" updated`) {
		t.Errorf("expected updated message, got: %s", result.Content)
	}

	cmd, _ := brain.ReadCommand(context.Background(), br, "upsert-cmd")
	if !strings.Contains(cmd.Content, "updated content") {
		t.Errorf("expected content containing 'updated content', got %q", cmd.Content)
	}
	if cmd.Description != "original" {
		t.Errorf("expected description unchanged, got %q", cmd.Description)
	}
}

func TestCommandUpsertDelete(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name":        "del-cmd",
		"description": "to delete",
		"content":     "bye",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-6", Name: "command_upsert", Arguments: string(createArgs),
	})

	// Only name = delete.
	args, _ := json.Marshal(map[string]string{"name": "del-cmd"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-7", Name: "command_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"del-cmd" deleted`) {
		t.Errorf("expected deleted message, got: %s", result.Content)
	}

	_, err = brain.ReadCommand(context.Background(), br, "del-cmd")
	if err == nil {
		t.Fatal("expected command to be deleted")
	}
}

func TestCommandUpsertDeleteNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-8", Name: "command_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for deleting nonexistent")
	}
}

func TestCommandUpsertMissingName(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": ""})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-9", Name: "command_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for empty name")
	}
}

func TestCommandUpsertInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "command_upsert", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestCommandToggle(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name":        "tog-cmd",
		"description": "toggle test",
		"content":     "toggle me",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-11", Name: "command_upsert", Arguments: string(createArgs),
	})

	args, _ := json.Marshal(map[string]any{"name": "tog-cmd", "enabled": false})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-12", Name: "command_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "disabled") {
		t.Errorf("expected 'disabled', got: %s", result.Content)
	}

	cmd, _ := brain.ReadCommand(context.Background(), br, "tog-cmd")
	if cmd.Enabled {
		t.Fatal("expected command to be disabled")
	}
}

func TestCommandToggleNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	args, _ := json.Marshal(map[string]any{"name": "nonexistent", "enabled": false})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-13", Name: "command_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing command")
	}
}

func TestCommandToggleInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-14", Name: "command_toggle", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestCommandExecute(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name":        "exec-cmd",
		"description": "exec test",
		"content":     "do something useful",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-15", Name: "command_upsert", Arguments: string(createArgs),
	})

	args, _ := json.Marshal(map[string]string{"name": "exec-cmd"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-16", Name: "command_execute", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "do something useful") {
		t.Errorf("expected content containing 'do something useful', got %q", result.Content)
	}
}

func TestCommandExecuteNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-17", Name: "command_execute", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing command")
	}
}

func TestCommandExecuteDisabled(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name":        "dis-cmd",
		"description": "disabled test",
		"content":     "should not run",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-18", Name: "command_upsert", Arguments: string(createArgs),
	})

	toggleArgs, _ := json.Marshal(map[string]any{"name": "dis-cmd", "enabled": false})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-19", Name: "command_toggle", Arguments: string(toggleArgs),
	})

	args, _ := json.Marshal(map[string]string{"name": "dis-cmd"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-20", Name: "command_execute", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for disabled command")
	}
	if !strings.Contains(result.Content, "disabled") {
		t.Errorf("expected 'disabled' error, got: %s", result.Content)
	}
}

func TestCommandExecuteInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterCommandTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-21", Name: "command_execute", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}
