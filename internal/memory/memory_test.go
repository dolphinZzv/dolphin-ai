package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"dolphin/internal/session"
	"dolphin/internal/types"
)

var errReadFailed = errors.New("read failed")

// testSessionStore is a minimal in-memory store for testing FileMemory.
type testSessionStore struct {
	sessions map[string]*session.Session
}

func (s *testSessionStore) Get(id string) *session.Session {
	return s.sessions[id]
}

func newTestStore(id string) *testSessionStore {
	sess := &session.Session{
		ID: id,
	}
	return &testSessionStore{
		sessions: map[string]*session.Session{id: sess},
	}
}

func TestNewFileMemory(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)
	if m == nil {
		t.Fatal("NewFileMemory returned nil")
	}
}

func TestFileMemoryWriteReadRoundTrip(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)

	now := time.Now().UTC().Truncate(time.Second)
	ctx := context.Background()

	msg := types.Message{
		Role:      types.RoleUser,
		Content:   "Hello, memory!",
		Timestamp: now,
	}

	if err := m.Write(ctx, "sess1", msg); err != nil {
		t.Fatal(err)
	}

	msgs, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != types.RoleUser {
		t.Errorf("Role = %q, want %q", msgs[0].Role, types.RoleUser)
	}
	if msgs[0].Content != "Hello, memory!" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "Hello, memory!")
	}
	if !msgs[0].Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", msgs[0].Timestamp, now)
	}
}

func TestFileMemoryWriteMultipleMessages(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		msg := types.Message{
			Role:      types.RoleUser,
			Content:   "msg",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
		if err := m.Write(ctx, "sess1", msg); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

func TestFileMemoryNonExistentSession(t *testing.T) {
	store := &testSessionStore{sessions: map[string]*session.Session{}}
	m := NewFileMemory(store)

	msgs, err := m.Read(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if msgs != nil {
		t.Errorf("expected nil for non-existent session, got %v", msgs)
	}
}

func TestFileMemoryWindowTruncation(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		if err := m.Write(ctx, "sess1", types.Message{
			Role: types.RoleUser, Content: "u", Timestamp: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := m.Write(ctx, "sess1", types.Message{
			Role: types.RoleAssistant, Content: "a", Timestamp: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	// 10 messages written, no window here (DroppingMemory handles that)
	if len(msgs) != 10 {
		t.Errorf("expected 10 messages, got %d", len(msgs))
	}
}

func TestFileMemoryMultipleSessions(t *testing.T) {
	s1 := &session.Session{ID: "sessA"}
	s2 := &session.Session{ID: "sessB"}
	store := &testSessionStore{
		sessions: map[string]*session.Session{"sessA": s1, "sessB": s2},
	}
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	m.Write(ctx, "sessA", types.Message{
		Role: types.RoleUser, Content: "from A", Timestamp: now,
	})
	m.Write(ctx, "sessB", types.Message{
		Role: types.RoleUser, Content: "from B", Timestamp: now,
	})

	msgsA, _ := m.Read(ctx, "sessA")
	msgsB, _ := m.Read(ctx, "sessB")

	if len(msgsA) != 1 || len(msgsB) != 1 {
		t.Fatalf("expected 1 msg each, got A=%d B=%d", len(msgsA), len(msgsB))
	}
	if msgsA[0].Content != "from A" {
		t.Errorf("sessA content = %q", msgsA[0].Content)
	}
	if msgsB[0].Content != "from B" {
		t.Errorf("sessB content = %q", msgsB[0].Content)
	}
}

func TestFileMemoryUsesJSONFile(t *testing.T) {
	// FileMemory no longer writes files — it stores in session.Data.
	// Test that writes go through the session store.
	sess := &session.Session{ID: "s1"}
	store := &testSessionStore{sessions: map[string]*session.Session{"s1": sess}}
	m := NewFileMemory(store)
	ctx := context.Background()

	_ = m.Write(ctx, "s1", types.Message{
		Role: types.RoleUser, Content: "test", Timestamp: time.Now(),
	})

	if sess.Get("memory") == nil {
		t.Fatal("expected memory key to be set in session data")
	}
}

func TestFileMemoryConcurrentWrites(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	done := make(chan struct{})
	go func() {
		m.Write(ctx, "sess1", types.Message{Role: types.RoleUser, Content: "a", Timestamp: now})
		close(done)
	}()
	m.Write(ctx, "sess1", types.Message{Role: types.RoleUser, Content: "b", Timestamp: now})
	<-done

	msgs, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 concurrent writes to both succeed, got %d", len(msgs))
	}
}

func TestNewDroppingMemory(t *testing.T) {
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 5)
	if dm == nil {
		t.Fatal("NewDroppingMemory returned nil")
	}
	if dm.window != 5 {
		t.Errorf("window = %d, want 5", dm.window)
	}
	if dm.inner != inner {
		t.Error("inner memory not set correctly")
	}
}

func TestDroppingMemoryWindowTruncation(t *testing.T) {
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 2) // window=2 => max 4 messages
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		_ = dm.Write(ctx, "s1", types.Message{
			Role: types.RoleUser, Content: "u", Timestamp: now,
		})
		_ = dm.Write(ctx, "s1", types.Message{
			Role: types.RoleAssistant, Content: "a", Timestamp: now,
		})
	}

	msgs, err := dm.Read(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	// 10 messages, window=2 => last 4
	if len(msgs) != 4 {
		t.Errorf("expected exactly 4 messages, got %d", len(msgs))
	}
}

func TestDroppingMemoryPreservesToolContext(t *testing.T) {
	inner := NewFileMemory(newTestStore("s2"))
	dm := NewDroppingMemory(inner, 1) // window=1 => max 2 messages, but must not orphan tool results
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Simulate a round with tool calls: user, assistant(tool_use), tool_result
	_ = dm.Write(ctx, "s2", types.Message{Role: types.RoleUser, Content: "u", Timestamp: now})
	_ = dm.Write(ctx, "s2", types.Message{Role: types.RoleAssistant, Content: "a", ToolCalls: []types.ToolCall{{ID: "t1", Name: "ls"}}, Timestamp: now})
	_ = dm.Write(ctx, "s2", types.Message{Role: types.RoleTool, ToolCallID: "t1", Content: "result", Timestamp: now})

	msgs, err := dm.Read(ctx, "s2")
	if err != nil {
		t.Fatal(err)
	}
	// window*2=2, but tool_result at index 2 must not be orphaned.
	// The trim should walk back to include the assistant message.
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != types.RoleAssistant {
		t.Errorf("first message should be assistant (has tool_use), got %s", msgs[0].Role)
	}
	if msgs[1].Role != types.RoleTool {
		t.Errorf("second message should be tool, got %s", msgs[1].Role)
	}
}

func TestDroppingMemoryNoTruncation(t *testing.T) {
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 0) // 0 = no truncation
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		_ = dm.Write(ctx, "s1", types.Message{
			Role: types.RoleUser, Content: "x", Timestamp: now,
		})
	}

	msgs, err := dm.Read(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages with window=0, got %d", len(msgs))
	}
}

func TestDroppingMemoryWriteDelegation(t *testing.T) {
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 5)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	msg := types.Message{Role: types.RoleUser, Content: "via dm", Timestamp: now}
	if err := dm.Write(ctx, "s1", msg); err != nil {
		t.Fatal(err)
	}

	// Verify via inner directly
	msgs, err := inner.Read(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "via dm" {
		t.Errorf("Content = %q, want 'via dm'", msgs[0].Content)
	}
}

func TestFileMemoryReadAfterJSONRoundTrip(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	store := &testSessionStore{sessions: map[string]*session.Session{"s1": sess}}
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := m.Write(ctx, "s1", types.Message{
		Role: types.RoleUser, Content: "original", Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(sess.Get("memory"))
	var raw []interface{}
	_ = json.Unmarshal(data, &raw)
	sess.Set("memory", raw)

	msgs, err := m.Read(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content != "original" {
		t.Fatalf("expected 1 message with content 'original', got %+v", msgs)
	}

	if err := m.Write(ctx, "s1", types.Message{
		Role: types.RoleAssistant, Content: "reply", Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err = m.Read(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestFileMemoryReadUnknownType(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	sess.Set("memory", "not-a-list")
	store := &testSessionStore{sessions: map[string]*session.Session{"s1": sess}}
	m := NewFileMemory(store)

	msgs, err := m.Read(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if msgs != nil {
		t.Fatal("expected nil for unknown type in Data[memory]")
	}
}

func TestDecodeMessages(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	raw := []interface{}{
		map[string]interface{}{
			"role":      "user",
			"content":   "hello",
			"timestamp": now.Format(time.RFC3339Nano),
		},
	}

	msgs, err := decodeMessages(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != types.RoleUser || msgs[0].Content != "hello" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestDecodeMessagesInvalidJSON(t *testing.T) {
	raw := []interface{}{map[string]interface{}{"role": make(chan int)}}
	_, err := decodeMessages(raw)
	if err == nil {
		t.Fatal("expected error for non-marshalable value")
	}
}

func TestFileMemoryReplace(t *testing.T) {
	store := newTestStore("sess1")
	m := NewFileMemory(store)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed with a few messages.
	for i := 0; i < 3; i++ {
		_ = m.Write(ctx, "sess1", types.Message{
			Role: types.RoleUser, Content: "old", Timestamp: now,
		})
	}
	before, _ := m.Read(ctx, "sess1")
	if len(before) != 3 {
		t.Fatalf("seed: expected 3 messages, got %d", len(before))
	}

	// Replace with a compacted [summary + tail] list.
	replaced := []types.Message{
		{Role: types.RoleUser, Content: "summary", IsSummary: true, Timestamp: now},
		{Role: types.RoleUser, Content: "tail", Timestamp: now},
	}
	if err := m.Replace(ctx, "sess1", replaced); err != nil {
		t.Fatal(err)
	}

	after, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("expected 2 messages after replace, got %d", len(after))
	}
	if !after[0].IsSummary || after[0].Content != "summary" {
		t.Errorf("first msg = %+v, want summary", after[0])
	}
	if after[1].Content != "tail" {
		t.Errorf("second msg = %+v, want tail", after[1])
	}
}

func TestDroppingMemoryReplaceDelegates(t *testing.T) {
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 5)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	replaced := []types.Message{
		{Role: types.RoleUser, Content: "summary", IsSummary: true, Timestamp: now},
	}
	if err := dm.Replace(ctx, "s1", replaced); err != nil {
		t.Fatal(err)
	}
	msgs, _ := dm.Read(ctx, "s1")
	if len(msgs) != 1 || !msgs[0].IsSummary {
		t.Fatalf("replace did not delegate to inner: %+v", msgs)
	}
}

type errReader struct{ Memory }

func (e *errReader) Read(_ context.Context, _ string) ([]types.Message, error) {
	return nil, errReadFailed
}

func TestDroppingMemoryReadError(t *testing.T) {
	dm := NewDroppingMemory(&errReader{}, 5)
	_, err := dm.Read(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error from inner Read")
	}
}

func TestFileMemoryWrite_DecodeError(t *testing.T) {
	sess := &session.Session{ID: "s1"}
	store := &testSessionStore{sessions: map[string]*session.Session{"s1": sess}}
	m := NewFileMemory(store)
	ctx := context.Background()

	sess.Set("memory", []interface{}{map[string]interface{}{"role": 42}})

	err := m.Write(ctx, "s1", types.Message{Role: types.RoleUser, Content: "x"})
	if err == nil {
		t.Fatal("expected error from corrupt memory data during Write")
	}
}

func TestFileMemoryReplace_NilMsgs(t *testing.T) {
	store := newTestStore("s1")
	m := NewFileMemory(store)
	if err := m.Replace(context.Background(), "s1", nil); err != nil {
		t.Fatal(err)
	}
	msgs, _ := m.Read(context.Background(), "s1")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after nil Replace, got %d", len(msgs))
	}
}

func TestFileMemoryReplace_NoSession(t *testing.T) {
	store := &testSessionStore{sessions: map[string]*session.Session{}}
	m := NewFileMemory(store)
	if err := m.Replace(context.Background(), "nonexistent", nil); err != nil {
		t.Fatal("expected no error for non-existent session")
	}
}

func TestDecodeMessages_UnmarshalError(t *testing.T) {
	raw := []interface{}{map[string]interface{}{"role": []interface{}{"nested", "array"}}}
	_, err := decodeMessages(raw)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestDroppingMemoryKeepsSummaryHead(t *testing.T) {
	// When the window would drop the leading summary, it must be kept.
	inner := NewFileMemory(newTestStore("s1"))
	dm := NewDroppingMemory(inner, 1) // window=1 => max 2 messages
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	msgs := []types.Message{
		{Role: types.RoleUser, Content: "summary", IsSummary: true, Timestamp: now},
		{Role: types.RoleUser, Content: "u1", Timestamp: now},
		{Role: types.RoleAssistant, Content: "a1", Timestamp: now},
		{Role: types.RoleUser, Content: "u2", Timestamp: now},
		{Role: types.RoleAssistant, Content: "a2", Timestamp: now},
	}
	if err := inner.Replace(ctx, "s1", msgs); err != nil {
		t.Fatal(err)
	}

	got, _ := dm.Read(ctx, "s1")
	// window=1 keeps 2 messages, but the summary head is preserved.
	if len(got) != 3 {
		t.Fatalf("expected 3 messages (summary + 2 tail), got %d", len(got))
	}
	if !got[0].IsSummary {
		t.Errorf("first message must be the summary, got %+v", got[0])
	}
}
