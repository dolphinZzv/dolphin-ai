package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir error: %v", err)
	}

	sess, err := mgr.NewSession(10)
	if err != nil {
		t.Fatalf("NewSession error: %v", err)
	}
	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.MaxLoop != 10 {
		t.Errorf("MaxLoop = %d, want 10", sess.MaxLoop)
	}

	got := mgr.Get(sess.ID)
	if got == nil {
		t.Fatal("Get returned nil for existing session")
	}
	if got.ID != sess.ID {
		t.Errorf("Get returned session with ID %q, want %q", got.ID, sess.ID)
	}
}

func TestManagerRemove(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	mgr.Remove(sess.ID)

	if got := mgr.Get(sess.ID); got != nil {
		t.Error("Get returned session after Remove")
	}
}

func TestManagerCleanup(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	mgr.NewSession(10)
	mgr.NewSession(10)
	mgr.Cleanup()

	if len(mgr.sessions) != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", len(mgr.sessions))
	}
}

func TestSessionLogMessage(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	sess.Turn = 1

	content := json.RawMessage(`{"text":"hello"}`)
	if err := sess.LogMessage("user", content); err != nil {
		t.Fatalf("LogMessage error: %v", err)
	}
	sess.Close()

	// Read the log file
	data, err := os.ReadFile(filepath.Join(dir, string(sess.ID)+".jsonl"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	var evt SessionEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if evt.Type != EventMessage {
		t.Errorf("event type = %q, want message", evt.Type)
	}
	if evt.Role != "user" {
		t.Errorf("role = %q, want user", evt.Role)
	}
	if evt.SessionID != sess.ID {
		t.Errorf("session_id mismatch")
	}
}

func TestSessionLogToolCall(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	sess.Turn = 1

	input := json.RawMessage(`{"command":"ls"}`)
	if err := sess.LogToolCall("shell", input); err != nil {
		t.Fatalf("LogToolCall error: %v", err)
	}
	sess.Close()

	data, _ := os.ReadFile(filepath.Join(dir, string(sess.ID)+".jsonl"))
	var evt SessionEvent
	json.Unmarshal(data, &evt)
	if evt.Type != EventToolCall {
		t.Errorf("type = %q, want tool_call", evt.Type)
	}
	if evt.ToolName != "shell" {
		t.Errorf("tool_name = %q", evt.ToolName)
	}
}

func TestSessionLogToolResult(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	sess.Turn = 1

	result := json.RawMessage(`{"output":"ok"}`)
	if err := sess.LogToolResult("shell", result, false); err != nil {
		t.Fatalf("LogToolResult error: %v", err)
	}
	sess.Close()

	data, _ := os.ReadFile(filepath.Join(dir, string(sess.ID)+".jsonl"))
	var evt SessionEvent
	json.Unmarshal(data, &evt)
	if evt.Type != EventToolResult {
		t.Errorf("type = %q, want tool_result", evt.Type)
	}
	if evt.IsError {
		t.Error("is_error should be false")
	}
}

func TestSessionLogSystem(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	if err := sess.LogSystem("test event"); err != nil {
		t.Fatalf("LogSystem error: %v", err)
	}
	sess.Close()

	data, _ := os.ReadFile(filepath.Join(dir, string(sess.ID)+".jsonl"))
	var evt SessionEvent
	json.Unmarshal(data, &evt)
	if evt.Type != EventSystem {
		t.Errorf("type = %q, want system", evt.Type)
	}
}

func TestLatestSessionEmpty(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	id, path, turns, err := mgr.LatestSession()
	if err != nil {
		t.Fatalf("LatestSession error: %v", err)
	}
	if id != "" || path != "" || turns != 0 {
		t.Errorf("expected empty result, got id=%q path=%q turns=%d", id, path, turns)
	}
}

func TestLatestSessionFindsMostRecent(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	// Create two sessions with staggered mtimes
	s1, _ := mgr.NewSession(10)
	writeEvent(s1, EventMessage, "user", `"hello"`, 1)
	s1.Close()
	time.Sleep(10 * time.Millisecond) // ensure distinct mtimes

	s2, _ := mgr.NewSession(10)
	writeEvent(s2, EventMessage, "user", `"world"`, 1)
	writeEvent(s2, EventMessage, "assistant", `"hi"`, 1)
	s2.Close()

	id, path, turns, err := mgr.LatestSession()
	if err != nil {
		t.Fatalf("LatestSession error: %v", err)
	}
	if id != s2.ID {
		t.Errorf("expected latest session %q, got %q", s2.ID, id)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if turns != 1 {
		t.Errorf("expected 1 turn, got %d", turns)
	}
}

func TestLatestSessionFiltersSummaryFiles(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	// Only a summary file (should be ignored)
	sumPath := filepath.Join(dir, "abc123-summary.json")
	os.WriteFile(sumPath, []byte("{}"), 0644)

	id, path, turns, err := mgr.LatestSession()
	if err != nil {
		t.Fatalf("LatestSession error: %v", err)
	}
	if id != "" || path != "" || turns != 0 {
		t.Errorf("expected empty result, got id=%q path=%q turns=%d", id, path, turns)
	}
}

func TestReadEvents(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, _ := mgr.NewSession(10)
	sess.Turn = 1
	sess.LogMessage("user", json.RawMessage(`"hello"`))
	sess.LogToolCall("shell", json.RawMessage(`{"command":"date"}`))
	sess.LogToolResult("shell", json.RawMessage(`{"output":"ok"}`), false)
	sess.LogSystem("system event")
	path := filepath.Join(dir, string(sess.ID)+".jsonl")
	sess.Close()

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Type != EventMessage || events[0].Role != "user" {
		t.Errorf("event[0] expected user message")
	}
	if events[1].Type != EventToolCall || events[1].ToolName != "shell" {
		t.Errorf("event[1] expected tool_call for shell")
	}
	if events[2].Type != EventToolResult || events[2].IsError {
		t.Errorf("event[2] expected non-error tool_result")
	}
	if events[3].Type != EventSystem {
		t.Errorf("event[3] expected system event")
	}
}

func TestReadEventsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	content := `{"ts":"2024-01-01T00:00:00Z","type":"message","role":"user","content":"hi"}
garbage line that is not json
{"ts":"2024-01-01T00:00:01Z","type":"message","role":"assistant","content":"hello"}
`
	os.WriteFile(path, []byte(content), 0644)

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 valid events, got %d", len(events))
	}
}

func TestReadEventsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte(""), 0644)

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestReadEventsFileNotFound(t *testing.T) {
	_, err := ReadEvents("/nonexistent/path.jsonl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// --- E2E: Session durability and integrity ---

func TestSessionLogSystemSpecialChars(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()
	sess, err := mgr.NewSession(10)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Message with double quotes, backslashes
	msg := `user said "hello", path is C:\test\data`
	if err := sess.LogSystem(msg); err != nil {
		t.Fatalf("LogSystem: %v", err)
	}
	path := filepath.Join(dir, string(sess.ID)+".jsonl")
	sess.Close()

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventSystem {
		t.Errorf("expected EventSystem, got %v", events[0].Type)
	}
	var content string
	if err := json.Unmarshal(events[0].Content, &content); err != nil {
		t.Fatalf("content is not valid JSON: %v (raw: %s)", err, string(events[0].Content))
	}
	if content != msg {
		t.Errorf("content = %q, want %q", content, msg)
	}
}

func TestSessionLogSystemInjectionAttempt(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()
	sess, err := mgr.NewSession(10)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Attempt JSON injection via LogSystem
	msg := `","type":"injected"}`
	if err := sess.LogSystem(msg); err != nil {
		t.Fatalf("LogSystem: %v", err)
	}
	path := filepath.Join(dir, string(sess.ID)+".jsonl")
	sess.Close()

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventSystem {
		t.Errorf("expected EventSystem, got %v (injection may have succeeded)", events[0].Type)
	}
}

func TestSessionFullLifecycle(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()

	sess, err := mgr.NewSession(50)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sess.ID == "" {
		t.Error("session ID is empty")
	}

	sess.LogMessage("user", json.RawMessage(`"hello"`))
	sess.LogToolCall("assistant", json.RawMessage(`{"name":"test_tool"}`))
	sess.LogToolResult("tool", json.RawMessage(`"result"`), false)
	sess.LogSystem("task completed")
	path := filepath.Join(dir, string(sess.ID)+".jsonl")
	sess.Close()

	events, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 4 {
		t.Errorf("expected 4 events, got %d", len(events))
	}

	types := []EventType{EventMessage, EventToolCall, EventToolResult, EventSystem}
	for i, e := range events {
		if e.Type != types[i] {
			t.Errorf("event[%d] type = %v, want %v", i, e.Type, types[i])
		}
		if e.SessionID != sess.ID {
			t.Errorf("event[%d] session ID mismatch", i)
		}
	}

	retrieved := mgr.Get(sess.ID)
	if retrieved == nil {
		t.Error("Get returned nil for valid session ID")
	}

	mgr.Remove(sess.ID)
	if mgr.Get(sess.ID) != nil {
		t.Error("Get should return nil after Remove")
	}
}

func TestSessionCountTurns(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureDir()
	sess, err := mgr.NewSession(10)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	for i := 1; i <= 3; i++ {
		sess.Turn = i
		sess.LogMessage("user", json.RawMessage(`"question"`))
		sess.LogMessage("assistant", json.RawMessage(`"answer"`))
	}
	path := filepath.Join(dir, string(sess.ID)+".jsonl")
	sess.Close()

	turns, err := countTurns(path)
	if err != nil {
		t.Fatalf("countTurns: %v", err)
	}
	if turns != 3 {
		t.Errorf("countTurns = %d, want 3", turns)
	}
}

// writeEvent is a helper that writes a SessionEvent to the session file.
func writeEvent(sess *Session, etype EventType, role, content string, turn int) {
	sess.Turn = turn
	switch etype {
	case EventMessage:
		sess.LogMessage(role, json.RawMessage(content))
	case EventToolCall:
		sess.LogToolCall(role, json.RawMessage(content))
	case EventToolResult:
		sess.LogToolResult(role, json.RawMessage(content), false)
	case EventSystem:
		sess.LogSystem(content)
	}
}

func TestManagerEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	mgr := NewManager(dir)
	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir error: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory was not created")
	}
}
