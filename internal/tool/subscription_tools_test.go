package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/brain"
	"dolphin/internal/types"
)

func setupTestBrainForSubscriptions(t *testing.T) *brain.Brain {
	t.Helper()
	dir := t.TempDir()
	b := brain.New(dir)
	if err := b.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return b
}

func TestRegisterSubscriptionTools(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"subscriptions_list":  false,
		"subscription_create": false,
		"subscription_update": false,
		"subscription_delete": false,
		"subscription_toggle": false,
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

func TestSubscriptionCreate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]string{
		"name":          "test-sub",
		"event_pattern": "file.*",
		"description":   "test subscription",
		"content":       "handle this",
	})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "subscription_create",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "created") {
		t.Errorf("expected 'created' in response, got: %s", result.Content)
	}

	// Verify
	sub, err := brain.ReadSubscription(context.Background(), br, "test-sub")
	if err != nil {
		t.Fatalf("ReadSubscription failed: %v", err)
	}
	if sub.EventPattern != "file.*" {
		t.Errorf("expected 'file.*', got %q", sub.EventPattern)
	}
	if !sub.Enabled {
		t.Error("expected enabled=true by default")
	}
}

func TestSubscriptionCreateWithFilterPath(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]string{
		"name":          "filtered",
		"event_pattern": "file.*",
		"filter_path":   "SOUL.md",
		"content":       "filtered",
	})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-2",
		Name:      "subscription_create",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	sub, _ := brain.ReadSubscription(context.Background(), br, "filtered")
	if sub.Filters.Path != "SOUL.md" {
		t.Errorf("expected filter path 'SOUL.md', got %q", sub.Filters.Path)
	}
}

func TestSubscriptionCreateDuplicate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	// Create once
	args, _ := json.Marshal(map[string]string{"name": "dup", "event_pattern": "file.*"})
	r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "subscription_create", Arguments: string(args),
	})

	// Create again — should error
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "subscription_create", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for duplicate create")
	}
	if !strings.Contains(result.Content, "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", result.Content)
	}
}

func TestSubscriptionCreateInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:        "call-5",
		Name:      "subscription_create",
		Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSubscriptionCreateMissingName(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]string{"event_pattern": "file.*"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-6", Name: "subscription_create", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for missing name")
	}
}

func TestSubscriptionList(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	// Create two subscriptions
	brain.WriteSubscription(context.Background(), br, brain.Subscription{Name: "sub-a", EventPattern: "file.*", Content: "a"})
	brain.WriteSubscription(context.Background(), br, brain.Subscription{Name: "sub-b", EventPattern: "llm.emit", Content: "b"})

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:   "call-7",
		Name: "subscriptions_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "sub-a") {
		t.Errorf("expected 'sub-a' in list, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "sub-b") {
		t.Errorf("expected 'sub-b' in list, got: %s", result.Content)
	}
}

func TestSubscriptionListEmpty(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID:   "call-8",
		Name: "subscriptions_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No subscriptions found") {
		t.Errorf("expected 'No subscriptions found', got: %s", result.Content)
	}
}

func TestSubscriptionUpdate(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	brain.WriteSubscription(context.Background(), br, brain.Subscription{
		Name: "updatable", EventPattern: "file.*", Content: "old", Enabled: true,
	})

	args, _ := json.Marshal(map[string]interface{}{
		"name":    "updatable",
		"content": "new content",
		"enabled": false,
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-9", Name: "subscription_update", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	sub, _ := brain.ReadSubscription(context.Background(), br, "updatable")
	if strings.TrimSpace(sub.Content) != "new content" {
		t.Errorf("expected 'new content', got %q", sub.Content)
	}
	if sub.Enabled {
		t.Error("expected enabled=false after update")
	}
}

func TestSubscriptionUpdateNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "subscription_update", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for updating nonexistent")
	}
}

func TestSubscriptionUpdateInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-11", Name: "subscription_update", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSubscriptionDelete(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	brain.WriteSubscription(context.Background(), br, brain.Subscription{Name: "delete-me", EventPattern: "file.*"})

	args, _ := json.Marshal(map[string]string{"name": "delete-me"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-12", Name: "subscription_delete", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "deleted") {
		t.Errorf("expected 'deleted' in response, got: %s", result.Content)
	}

	_, err = brain.ReadSubscription(context.Background(), br, "delete-me")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSubscriptionDeleteNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-13", Name: "subscription_delete", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for deleting nonexistent")
	}
}

func TestSubscriptionDeleteInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-14", Name: "subscription_delete", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSubscriptionToggleEnable(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	brain.WriteSubscription(context.Background(), br, brain.Subscription{
		Name: "togglable", EventPattern: "file.*", Enabled: false,
	})

	args, _ := json.Marshal(map[string]interface{}{
		"name": "togglable", "enabled": true,
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-15", Name: "subscription_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "enabled") {
		t.Errorf("expected 'enabled' in response, got: %s", result.Content)
	}

	sub, _ := brain.ReadSubscription(context.Background(), br, "togglable")
	if !sub.Enabled {
		t.Error("expected enabled=true after toggle")
	}
}

func TestSubscriptionToggleDisable(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	brain.WriteSubscription(context.Background(), br, brain.Subscription{
		Name: "togglable", EventPattern: "file.*", Enabled: true,
	})

	args, _ := json.Marshal(map[string]interface{}{
		"name": "togglable", "enabled": false,
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-16", Name: "subscription_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "disabled") {
		t.Errorf("expected 'disabled' in response, got: %s", result.Content)
	}

	sub, _ := brain.ReadSubscription(context.Background(), br, "togglable")
	if sub.Enabled {
		t.Error("expected enabled=false after toggle")
	}
}

func TestSubscriptionToggleNotFound(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	args, _ := json.Marshal(map[string]interface{}{
		"name": "nonexistent", "enabled": true,
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-17", Name: "subscription_toggle", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for toggling nonexistent")
	}
}

func TestSubscriptionToggleInvalidArgs(t *testing.T) {
	r := NewRegistry()
	br := setupTestBrainForSubscriptions(t)
	RegisterSubscriptionTools(r, br)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-18", Name: "subscription_toggle", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}
