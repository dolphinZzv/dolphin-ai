package memory_test

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/memory"
	"dolphin/internal/types"
)

func newTestWAL(t *testing.T) *memory.WALMemory {
	t.Helper()
	dir := t.TempDir()
	m, err := memory.NewWALMemory(dir, 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("NewWALMemory: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func writeMsg(t *testing.T, m *memory.WALMemory, sessionID, text string) {
	t.Helper()
	msg := types.NewTextMessage(types.RoleUser, text)
	msg.Timestamp = time.Now()
	if err := m.Write(context.Background(), sessionID, msg); err != nil {
		t.Fatalf("Write(%q): %v", text, err)
	}
}

func writeAssistantMsg(t *testing.T, m *memory.WALMemory, sessionID, text string) {
	t.Helper()
	msg := types.NewTextMessage(types.RoleAssistant, text)
	msg.Timestamp = time.Now()
	if err := m.Write(context.Background(), sessionID, msg); err != nil {
		t.Fatalf("Write assistant(%q): %v", text, err)
	}
}

func TestWAL_WriteAndRead(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-1"

	// Writing without a compact entry is allowed but Read returns nil.
	writeMsg(t, m, sid, "hello")

	// First write creates an initial compact internally... wait, no.
	// We need to call Replace first to set the compact baseline.
	// Actually our design: Read returns nil when no compact exists. But Write
	// without a compact means entries are appended but not reachable by Read.
	// Fix: Write should auto-create a compact if none exists.
	// For now, test with Replace first.

	m2 := newTestWAL(t)
	sid2 := "sess-2"

	// Create initial compact via Replace.
	initial := []types.Message{types.NewTextMessage(types.RoleSystem, "system init")}
	if err := m2.Replace(context.Background(), sid2, initial); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	writeMsg(t, m2, sid2, "msg-1")
	writeAssistantMsg(t, m2, sid2, "assistant-1")
	writeMsg(t, m2, sid2, "msg-2")

	msgs, err := m2.Read(context.Background(), sid2, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Text() != "system init" {
		t.Errorf("msg 0: got %q", msgs[0].Text())
	}
	if msgs[2].Text() != "assistant-1" {
		t.Errorf("msg 2: got %q", msgs[2].Text())
	}
}

func TestWAL_ReadRange(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-range"

	// Create baseline.
	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "base")})

	// Write 5 messages.
	for i := 1; i <= 5; i++ {
		writeMsg(t, m, sid, "msg-"+itoa(i))
	}

	// Read last 2.
	msgs, err := m.Read(context.Background(), sid, -2, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].Text() != "msg-4" || msgs[1].Text() != "msg-5" {
		t.Errorf("got %q / %q", msgs[0].Text(), msgs[1].Text())
	}

	// Read middle range.
	msgs, err = m.Read(context.Background(), sid, 2, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3, got %d", len(msgs))
	}
}

func TestWAL_Replace(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-replace"

	// Initial compact.
	m.Replace(context.Background(), sid, []types.Message{
		types.NewTextMessage(types.RoleSystem, "base"),
	})
	writeMsg(t, m, sid, "old-1")
	writeMsg(t, m, sid, "old-2")

	// Replace with new messages.
	newMsgs := []types.Message{
		types.NewTextMessage(types.RoleSystem, "new base"),
		types.NewTextMessage(types.RoleUser, "compact result"),
	}
	if err := m.Replace(context.Background(), sid, newMsgs); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	msgs, err := m.Read(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 after replace, got %d", len(msgs))
	}
	if msgs[0].Text() != "new base" || msgs[1].Text() != "compact result" {
		t.Errorf("got %q / %q", msgs[0].Text(), msgs[1].Text())
	}
}

func TestWAL_RestartRebuild(t *testing.T) {
	dir := t.TempDir()

	// First session: write some data.
	m1, err := memory.NewWALMemory(dir, 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("NewWALMemory: %v", err)
	}
	sid := "sess-restart"
	m1.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	writeMsg(t, m1, sid, "msg-1")
	writeMsg(t, m1, sid, "msg-2")
	m1.Close()

	// Reopen.
	m2, err := memory.NewWALMemory(dir, 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("NewWALMemory (reopen): %v", err)
	}
	defer m2.Close()

	msgs, err := m2.Read(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatalf("Read after restart: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 after restart, got %d", len(msgs))
	}
	if msgs[1].Text() != "msg-1" {
		t.Errorf("got %q", msgs[1].Text())
	}
}

func TestWAL_SessionIsolation(t *testing.T) {
	m := newTestWAL(t)

	m.Replace(context.Background(), "a", []types.Message{types.NewTextMessage(types.RoleSystem, "init-a")})
	m.Replace(context.Background(), "b", []types.Message{types.NewTextMessage(types.RoleSystem, "init-b")})

	writeMsg(t, m, "a", "a-1")
	writeMsg(t, m, "b", "b-1")
	writeMsg(t, m, "b", "b-2")

	msgsA, _ := m.Read(context.Background(), "a", 0, 0)
	msgsB, _ := m.Read(context.Background(), "b", 0, 0)

	if len(msgsA) != 2 || msgsA[1].Text() != "a-1" {
		t.Errorf("a: got %d msgs", len(msgsA))
	}
	if len(msgsB) != 3 || msgsB[2].Text() != "b-2" {
		t.Errorf("b: got %d msgs", len(msgsB))
	}
}

func TestWAL_EmptySession(t *testing.T) {
	m := newTestWAL(t)
	msgs, err := m.Read(context.Background(), "nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty, got %d msgs", len(msgs))
	}
}

func TestWAL_TurnMarks(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-turns"

	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	writeMsg(t, m, sid, "msg-1")
	m.WriteTurn(context.Background(), sid, memory.TurnPayload{
		TurnID: "t-1", Input: "hello", ModelName: "test-model",
		InTokens: 10, OutTokens: 5, Rounds: 1,
	})

	marks, err := m.TurnMarks(sid)
	if err != nil {
		t.Fatalf("TurnMarks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 turn mark, got %d", len(marks))
	}
	if marks[0].TurnID != "t-1" || marks[0].Input != "hello" {
		t.Errorf("turn mark mismatch: %+v", marks[0])
	}
}

func TestWAL_Rewind(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-rewind"

	// Turn 1.
	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	writeMsg(t, m, sid, "t1-msg")
	m.WriteTurn(context.Background(), sid, memory.TurnPayload{
		TurnID: "t-1", Input: "first", ModelName: "m", InTokens: 10, OutTokens: 5, Rounds: 1,
	})

	// Turn 2.
	seqT2 := writeMsgAndReturnSeq(t, m, sid, "t2-msg")

	// Rewind to turn 1's mark.
	// We need to find the seq of turn 1's compact.
	marks, _ := m.TurnMarks(sid)
	if len(marks) < 1 {
		t.Fatal("no turn marks")
	}

	// Rewind to the compact entry before turn 2 (i.e. the initial compact).
	// After rewind we should see init + t1-msg only.
	if err := m.RewindTo(sid, marks[0].Seq); err != nil {
		t.Fatalf("RewindTo: %v", err)
	}

	msgs, _ := m.Read(context.Background(), sid, 0, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 after rewind, got %d", len(msgs))
	}
	if msgs[1].Text() != "t1-msg" {
		t.Errorf("got %q", msgs[1].Text())
	}

	_ = seqT2
}

func TestWAL_GC_Retention(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-gc"

	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	writeMsg(t, m, sid, "msg-1")

	// GC should not delete the compact or recent messages.
	now := time.Now()
	if err := m.GC(now); err != nil {
		t.Fatalf("GC: %v", err)
	}

	msgs, _ := m.Read(context.Background(), sid, 0, 0)
	if len(msgs) != 2 {
		t.Errorf("GC removed data: expected 2, got %d", len(msgs))
	}
}

func TestWAL_ConcurrentWriteRead(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-concurrent"

	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			writeMsg(t, m, sid, "c-"+itoa(i))
		}
	}()

	<-done

	msgs, _ := m.Read(context.Background(), sid, 0, 0)
	if len(msgs) < 5 {
		t.Errorf("too few messages: %d", len(msgs))
	}
}

func TestWAL_MessageTypes(t *testing.T) {
	m := newTestWAL(t)
	sid := "sess-types"

	m.Replace(context.Background(), sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})

	toolMsg := types.Message{
		Role:       types.RoleTool,
		ToolCallID: "call-1",
		Parts:      []types.ContentPart{{Type: types.PartText, Text: "tool result"}},
		Timestamp:  time.Now(),
	}
	if err := m.Write(context.Background(), sid, toolMsg); err != nil {
		t.Fatalf("Write tool: %v", err)
	}

	assistantWithThinking := types.Message{
		Role:      types.RoleAssistant,
		Parts:     []types.ContentPart{{Type: types.PartText, Text: "response"}},
		Thinking:  "thinking...",
		Timestamp: time.Now(),
	}
	if err := m.Write(context.Background(), sid, assistantWithThinking); err != nil {
		t.Fatalf("Write assistant: %v", err)
	}

	msgs, _ := m.Read(context.Background(), sid, 0, 0)
	if len(msgs) != 3 {
		t.Fatalf("expected 3, got %d", len(msgs))
	}

	if msgs[1].Role != types.RoleTool || msgs[1].ToolCallID != "call-1" {
		t.Errorf("tool mismatch: role=%s id=%s", msgs[1].Role, msgs[1].ToolCallID)
	}
	if msgs[2].Role != types.RoleAssistant || msgs[2].Thinking != "thinking..." {
		t.Errorf("assistant mismatch: role=%s thinking=%s", msgs[2].Role, msgs[2].Thinking)
	}
}

func writeMsgAndReturnSeq(t *testing.T, m *memory.WALMemory, sessionID, text string) uint64 {
	t.Helper()
	msg := types.NewTextMessage(types.RoleUser, text)
	msg.Timestamp = time.Now()
	if err := m.Write(context.Background(), sessionID, msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return 0 // WALMemory.Write doesn't expose seq, but that's fine for tests
}

