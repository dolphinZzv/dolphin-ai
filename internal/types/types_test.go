package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRoleConstants(t *testing.T) {
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q, want %q", RoleUser, "user")
	}
	if RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q, want %q", RoleAssistant, "assistant")
	}
	if RoleTool != "tool" {
		t.Errorf("RoleTool = %q, want %q", RoleTool, "tool")
	}
	if RoleSystem != "system" {
		t.Errorf("RoleSystem = %q, want %q", RoleSystem, "system")
	}
}

func TestMessageCreation(t *testing.T) {
	now := time.Now()
	msg := Message{
		Role:      RoleUser,
		Content:   "Hello, world!",
		Timestamp: now,
	}
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", msg.Content, "Hello, world!")
	}
	if !msg.Timestamp.Equal(now) {
		t.Error("Timestamp mismatch")
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	now := time.Now().Round(time.Microsecond)
	original := Message{
		Role:      RoleUser,
		Content:   "Hello, world!",
		Timestamp: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Role != original.Role {
		t.Errorf("Role = %q, want %q", decoded.Role, original.Role)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: %v vs %v", decoded.Timestamp, original.Timestamp)
	}
}

func TestMessageWithToolCallsJSON(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "Let me check that",
		ToolCalls: []ToolCall{
			{ID: "call-1", Name: "get_weather", Arguments: `{"city":"NYC"}`},
		},
		Timestamp: time.Now().Round(time.Microsecond),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall, got %d", len(decoded.ToolCalls))
	}
	if decoded.ToolCalls[0].ID != "call-1" {
		t.Errorf("ToolCall ID = %q, want %q", decoded.ToolCalls[0].ID, "call-1")
	}
	if decoded.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCall Name = %q, want %q", decoded.ToolCalls[0].Name, "get_weather")
	}
	if decoded.ToolCalls[0].Arguments != `{"city":"NYC"}` {
		t.Errorf("ToolCall Arguments = %q", decoded.ToolCalls[0].Arguments)
	}
}

func TestMessageMultipleToolCallsJSON(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "Multiple calls",
		ToolCalls: []ToolCall{
			{ID: "c1", Name: "tool1", Arguments: `{"a":1}`},
			{ID: "c2", Name: "tool2", Arguments: `{"b":2}`},
		},
		Timestamp: time.Now().Round(time.Microsecond),
	}

	data, _ := json.Marshal(msg)
	var decoded Message
	json.Unmarshal(data, &decoded)

	if len(decoded.ToolCalls) != 2 {
		t.Fatalf("expected 2 ToolCalls, got %d", len(decoded.ToolCalls))
	}
}

func TestMessageWithToolCallID(t *testing.T) {
	msg := Message{
		Role:       RoleTool,
		Content:    "Tool result content",
		ToolCallID: "call-1",
		Timestamp:  time.Now().Round(time.Microsecond),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ToolCallID != "call-1" {
		t.Errorf("ToolCallID = %q, want %q", decoded.ToolCallID, "call-1")
	}
	if decoded.Role != RoleTool {
		t.Errorf("Role = %q, want %q", decoded.Role, RoleTool)
	}
	if decoded.Content != "Tool result content" {
		t.Errorf("Content = %q", decoded.Content)
	}
}

func TestMessageAllRoles(t *testing.T) {
	now := time.Now().Round(time.Microsecond)
	roles := []struct {
		name string
		role Role
	}{
		{"user", RoleUser},
		{"assistant", RoleAssistant},
		{"tool", RoleTool},
		{"system", RoleSystem},
	}
	for _, r := range roles {
		t.Run(r.name, func(t *testing.T) {
			msg := Message{Role: r.role, Content: "test", Timestamp: now}
			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Message
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Role != r.role {
				t.Errorf("Role = %q, want %q", decoded.Role, r.role)
			}
		})
	}
}

func TestToolCallJSONRoundTrip(t *testing.T) {
	original := ToolCall{
		ID:        "tc-1",
		Name:      "test_tool",
		Arguments: `{"key":"value"}`,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolCall
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Arguments != original.Arguments {
		t.Errorf("Arguments = %q, want %q", decoded.Arguments, original.Arguments)
	}
}

func TestToolResultJSONRoundTrip(t *testing.T) {
	original := ToolResult{
		ToolCallID: "tc-1",
		Content:    "Operation completed",
		IsError:    false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ToolCallID != original.ToolCallID {
		t.Errorf("ToolCallID = %q, want %q", decoded.ToolCallID, original.ToolCallID)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
	if decoded.IsError != original.IsError {
		t.Errorf("IsError = %v, want %v", decoded.IsError, original.IsError)
	}
}

func TestToolResultWithError(t *testing.T) {
	original := ToolResult{
		ToolCallID: "tc-1",
		Content:    "Something failed",
		IsError:    true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !decoded.IsError {
		t.Error("IsError should be true")
	}
	if decoded.Content != "Something failed" {
		t.Errorf("Content = %q", decoded.Content)
	}
}

func TestToolDefJSONRoundTrip(t *testing.T) {
	original := ToolDef{
		Name:        "my_tool",
		Description: "A useful tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolDef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Description != original.Description {
		t.Errorf("Description = %q, want %q", decoded.Description, original.Description)
	}
	if string(decoded.Schema) != string(original.Schema) {
		t.Errorf("Schema = %s, want %s", string(decoded.Schema), string(original.Schema))
	}
}

func TestMessageZeroValues(t *testing.T) {
	msg := Message{}
	if msg.Role != "" {
		t.Errorf("default Role should be empty, got %q", msg.Role)
	}
	if msg.ToolCallID != "" {
		t.Errorf("default ToolCallID should be empty, got %q", msg.ToolCallID)
	}
	if msg.ToolCalls != nil {
		t.Errorf("default ToolCalls should be nil, got %v", msg.ToolCalls)
	}
}

func TestToolCallZeroValues(t *testing.T) {
	tc := ToolCall{}
	if tc.ID != "" || tc.Name != "" || tc.Arguments != "" {
		t.Error("ToolCall zero values should be empty strings")
	}
}

func TestToolResultZeroValues(t *testing.T) {
	tr := ToolResult{}
	if tr.ToolCallID != "" || tr.Content != "" {
		t.Error("ToolResult zero values should be empty strings")
	}
	if tr.IsError {
		t.Error("ToolResult IsError should default to false")
	}
}

func TestMessageOmitEmptyFields(t *testing.T) {
	msg := Message{
		Role:      RoleUser,
		Content:   "Hello",
		Timestamp: time.Now().Round(time.Microsecond),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// tool_call_id and tool_calls should be omitted when empty
	if _, exists := raw["tool_call_id"]; exists {
		t.Error("tool_call_id should be omitted when empty")
	}
	if _, exists := raw["tool_calls"]; exists {
		t.Error("tool_calls should be omitted when empty")
	}
}

func TestToolResultOmitEmptyIsError(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "tc-1",
		Content:    "ok",
		IsError:    false,
	}

	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// is_error should be omitted when false
	if _, exists := raw["is_error"]; exists {
		t.Error("is_error should be omitted when false")
	}
}
