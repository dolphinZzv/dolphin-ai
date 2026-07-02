package memory_test

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/memory"
	"dolphin/internal/types"
)

func newTestDB(t *testing.T) *memory.SQLiteMemory {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/test.db"
	db, err := memory.NewSQLiteMemory(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteMemory: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedMessages(t *testing.T, mem memory.Memory, sessionID string, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		msg := types.NewTextMessage(types.RoleUser, "msg-"+itoa(i))
		if err := mem.Write(context.Background(), sessionID, msg); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
}

// Simple itoa for test file (no fmt dependency).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestSQLite_WriteAndRead(t *testing.T) {
	db := newTestDB(t)
	sid := "sess-1"

	// Write a few messages.
	seedMessages(t, db, sid, 3)

	// Read all.
	msgs, err := db.Read(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Text() != "msg-1" {
		t.Errorf("expected msg-1, got %q", msgs[0].Text())
	}
	if msgs[2].Text() != "msg-3" {
		t.Errorf("expected msg-3, got %q", msgs[2].Text())
	}
}

func TestSQLite_ReadRange(t *testing.T) {
	db := newTestDB(t)
	sid := "sess-2"
	seedMessages(t, db, sid, 5)

	// Read last 2 (start=-2, end=0).
	msgs, err := db.Read(context.Background(), sid, -2, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].Text() != "msg-4" || msgs[1].Text() != "msg-5" {
		t.Errorf("got %q / %q", msgs[0].Text(), msgs[1].Text())
	}

	// Read middle (start=1, end=4).
	msgs, err = db.Read(context.Background(), sid, 1, 4)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3, got %d", len(msgs))
	}
}

func TestSQLite_ReadEmpty(t *testing.T) {
	db := newTestDB(t)
	msgs, err := db.Read(context.Background(), "nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty, got %d", len(msgs))
	}
}

func TestSQLite_Replace(t *testing.T) {
	db := newTestDB(t)
	sid := "sess-3"
	seedMessages(t, db, sid, 3)

	// Replace with 2 new messages.
	newMsgs := []types.Message{
		types.NewTextMessage(types.RoleUser, "new-1"),
		types.NewTextMessage(types.RoleAssistant, "new-2"),
	}
	if err := db.Replace(context.Background(), sid, newMsgs); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	msgs, err := db.Read(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].Text() != "new-1" {
		t.Errorf("got %q", msgs[0].Text())
	}
}

func TestSQLite_Isolation(t *testing.T) {
	db := newTestDB(t)
	seedMessages(t, db, "a", 2)
	seedMessages(t, db, "b", 3)

	msgsA, _ := db.Read(context.Background(), "a", 0, 0)
	msgsB, _ := db.Read(context.Background(), "b", 0, 0)
	if len(msgsA) != 2 || len(msgsB) != 3 {
		t.Errorf("isolation broken: a=%d b=%d", len(msgsA), len(msgsB))
	}
}

func TestSQLite_WithDroppingMemory(t *testing.T) {
	db := newTestDB(t)
	sid := "sess-drop"
	seedMessages(t, db, sid, 10)

	dm := memory.NewDroppingMemory(db, 3) // keep at most 6 messages

	msgs, err := dm.Read(context.Background(), sid, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 6 {
		t.Errorf("expected 6 (window=3*2), got %d", len(msgs))
	}
	// Should have kept the last 6: msg-5 through msg-10.
	for i, m := range msgs {
		expected := "msg-" + itoa(5+i)
		if m.Text() != expected {
			t.Errorf("pos %d: expected %q, got %q", i, expected, m.Text())
		}
	}

	// Write through DroppingMemory passes through to inner.
	newMsg := types.NewTextMessage(types.RoleUser, "new-msg")
	if err := dm.Write(context.Background(), sid, newMsg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	all, _ := db.Read(context.Background(), sid, 0, 0)
	if len(all) != 11 {
		t.Errorf("expected 11, got %d", len(all))
	}
}

func TestSQLite_MessageTypes(t *testing.T) {
	db := newTestDB(t)
	sid := "sess-types"

	msgs := []types.Message{
		{Role: types.RoleSystem, Parts: []types.ContentPart{{Type: types.PartText, Text: "system msg"}}, Timestamp: timeNow()},
		{Role: types.RoleUser, Parts: []types.ContentPart{{Type: types.PartText, Text: "user msg"}}, Timestamp: timeNow()},
		{Role: types.RoleAssistant, Parts: []types.ContentPart{{Type: types.PartText, Text: "assistant msg"}}, Thinking: "thinking...", Timestamp: timeNow()},
		{Role: types.RoleTool, ToolCallID: "call-1", Parts: []types.ContentPart{{Type: types.PartText, Text: "tool result"}}, Timestamp: timeNow()},
	}
	for _, msg := range msgs {
		if err := db.Write(context.Background(), sid, msg); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	got, _ := db.Read(context.Background(), sid, 0, 0)
	if len(got) != 4 {
		t.Fatalf("expected 4, got %d", len(got))
	}
	if got[0].Role != types.RoleSystem || got[0].Text() != "system msg" {
		t.Errorf("system mismatch")
	}
	if got[1].Role != types.RoleUser || got[1].Text() != "user msg" {
		t.Errorf("user mismatch")
	}
	if got[2].Role != types.RoleAssistant || got[2].Text() != "assistant msg" || got[2].Thinking != "thinking..." {
		t.Errorf("assistant mismatch: %q / thinking=%q", got[2].Text(), got[2].Thinking)
	}
	if got[3].Role != types.RoleTool || got[3].ToolCallID != "call-1" || got[3].Text() != "tool result" {
		t.Errorf("tool mismatch: id=%q text=%q", got[3].ToolCallID, got[3].Text())
	}
}

func timeNow() time.Time { return time.Now() }
