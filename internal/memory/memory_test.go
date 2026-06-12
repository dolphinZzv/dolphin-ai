package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dolphin/internal/types"
)

func TestNewFileMemory(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 10)
	if m == nil {
		t.Fatal("NewFileMemory returned nil")
	}
	if m.dir != dir {
		t.Errorf("dir = %q, want %q", m.dir, dir)
	}
	if m.window != 10 {
		t.Errorf("window = %d, want 10", m.window)
	}
}

func TestFileMemoryWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)

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
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)
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
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)

	msgs, err := m.Read(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if msgs != nil {
		t.Errorf("expected nil for non-existent session, got %v", msgs)
	}
}

func TestFileMemoryWindowTruncation(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 2) // window=2 => max 4 messages
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
	// 10 messages written, window=2 => max 4
	if len(msgs) > 4 {
		t.Errorf("expected at most 4 messages with window=2, got %d", len(msgs))
	}
}

func TestFileMemoryWindowZero(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 0) // 0 = no truncation
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 10; i++ {
		m.Write(ctx, "sess1", types.Message{
			Role: types.RoleUser, Content: "x", Timestamp: now,
		})
	}

	msgs, err := m.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 10 {
		t.Errorf("expected 10 messages with window=0, got %d", len(msgs))
	}
}

func TestFileMemoryMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)
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
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)
	ctx := context.Background()

	m.Write(ctx, "s1", types.Message{
		Role: types.RoleUser, Content: "test", Timestamp: time.Now(),
	})

	// File should be .json, not .md
	_, err := os.Stat(filepath.Join(dir, "s1.json"))
	if err != nil {
		t.Fatalf("expected s1.json to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "s1.md")); err == nil {
		t.Error("s1.md should not exist")
	}
}

func TestFileMemoryConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	m := NewFileMemory(dir, 0)
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
	inner := NewFileMemory(t.TempDir(), 0)
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
	dir := t.TempDir()
	inner := NewFileMemory(dir, 0)    // inner has no truncation
	dm := NewDroppingMemory(inner, 2) // window=2 => max 4 messages
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		dm.Write(ctx, "sess1", types.Message{
			Role: types.RoleUser, Content: "u", Timestamp: now,
		})
		dm.Write(ctx, "sess1", types.Message{
			Role: types.RoleAssistant, Content: "a", Timestamp: now,
		})
	}

	msgs, err := dm.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	// 10 messages, window=2 => last 4
	if len(msgs) > 4 {
		t.Errorf("expected at most 4 messages, got %d", len(msgs))
	}
	if len(msgs) != 4 {
		t.Errorf("expected exactly 4 messages, got %d", len(msgs))
	}
}

func TestDroppingMemoryNoTruncation(t *testing.T) {
	dir := t.TempDir()
	inner := NewFileMemory(dir, 0)
	dm := NewDroppingMemory(inner, 0) // 0 = no truncation
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		dm.Write(ctx, "sess1", types.Message{
			Role: types.RoleUser, Content: "x", Timestamp: now,
		})
	}

	msgs, err := dm.Read(ctx, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages with window=0, got %d", len(msgs))
	}
}

func TestDroppingMemoryWriteDelegation(t *testing.T) {
	dir := t.TempDir()
	inner := NewFileMemory(dir, 0)
	dm := NewDroppingMemory(inner, 5)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	msg := types.Message{Role: types.RoleUser, Content: "via dm", Timestamp: now}
	if err := dm.Write(ctx, "sess1", msg); err != nil {
		t.Fatal(err)
	}

	// Verify via inner directly
	msgs, err := inner.Read(ctx, "sess1")
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
