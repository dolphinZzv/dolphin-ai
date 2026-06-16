package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

func TestRegisterScriptTools(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"scripts_list":  false,
		"script_upsert": false,
		"script_toggle": false,
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

func TestScriptsList(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	brain.WriteScript(context.Background(), br, brain.Script{
		Name: "test-scr", Description: "a script", Enabled: true, Content: "do stuff",
	})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-1", Name: "scripts_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test-scr") {
		t.Errorf("expected test-scr in list, got: %s", result.Content)
	}
}

func TestScriptsListEmpty(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-2", Name: "scripts_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "No scripts found" {
		t.Errorf("expected 'No scripts found', got: %s", result.Content)
	}
}

func TestScriptUpsertCreate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	args, _ := json.Marshal(map[string]string{
		"name": "my-scr", "description": "my script", "content": "run this",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "script_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"my-scr" created`) {
		t.Errorf("expected creation message, got: %s", result.Content)
	}

	scr, err := brain.ReadScript(context.Background(), br, "my-scr")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scr.Content, "run this") {
		t.Errorf("expected 'run this' in script, got %q", scr.Content)
	}
}

func TestScriptUpsertUpdate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name": "upd-scr", "description": "original", "content": "original",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "script_upsert", Arguments: string(createArgs),
	})

	updateArgs, _ := json.Marshal(map[string]string{
		"name": "upd-scr", "content": "updated",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-5", Name: "script_upsert", Arguments: string(updateArgs),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"upd-scr" updated`) {
		t.Errorf("expected updated message, got: %s", result.Content)
	}
}

func TestScriptUpsertDelete(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name": "del-scr", "description": "to delete", "content": "bye",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-6", Name: "script_upsert", Arguments: string(createArgs),
	})

	args, _ := json.Marshal(map[string]string{"name": "del-scr"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-7", Name: "script_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"del-scr" deleted`) {
		t.Errorf("expected deleted message, got: %s", result.Content)
	}

	_, err = brain.ReadScript(context.Background(), br, "del-scr")
	if err == nil {
		t.Fatal("expected script to be deleted")
	}
}

func TestScriptUpsertDeleteNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-8", Name: "script_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for deleting nonexistent")
	}
}

func TestScriptUpsertMissingName(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": ""})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-9", Name: "script_upsert", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for empty name")
	}
}

func TestScriptUpsertInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "script_upsert", Arguments: "not json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestScriptToggle(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	createArgs, _ := json.Marshal(map[string]string{
		"name": "tog-scr", "description": "toggle test", "content": "toggle me",
	})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-11", Name: "script_upsert", Arguments: string(createArgs),
	})

	args, _ := json.Marshal(map[string]any{"name": "tog-scr", "enabled": false})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-12", Name: "script_toggle", Arguments: string(args),
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

	scr, _ := brain.ReadScript(context.Background(), br, "tog-scr")
	if scr.Enabled {
		t.Fatal("expected script to be disabled")
	}
}

func TestScriptToggleNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	args, _ := json.Marshal(map[string]any{"name": "nonexistent", "enabled": false})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-13", Name: "script_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing script")
	}
}

func TestScriptToggleInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForTools(t)
	RegisterScriptTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-14", Name: "script_toggle", Arguments: "not json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}
