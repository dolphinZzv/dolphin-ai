package dump

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dolphin/internal/types"
)

func TestNewRecorder(t *testing.T) {
	r := NewRecorder("/tmp/dumps")
	if r == nil {
		t.Fatal("expected non-nil Recorder")
	}
	if r.Dir() != "/tmp/dumps" {
		t.Errorf("expected dir '/tmp/dumps', got %q", r.Dir())
	}
}

func TestRecordAndLast(t *testing.T) {
	r := NewRecorder("/tmp/dumps")

	// Last on empty recorder returns nil
	if got := r.Last("sess_1"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	dump := &TurnDump{
		SessionID: "sess_1",
		ModelName: "gpt-4",
		Input:     "hello",
		Timestamp: time.Now(),
	}
	r.Record(dump)

	got := r.Last("sess_1")
	if got == nil {
		t.Fatal("expected non-nil dump")
	}
	if got.SessionID != "sess_1" {
		t.Errorf("expected 'sess_1', got %q", got.SessionID)
	}
	if got.ModelName != "gpt-4" {
		t.Errorf("expected 'gpt-4', got %q", got.ModelName)
	}
	if got.Input != "hello" {
		t.Errorf("expected 'hello', got %q", got.Input)
	}
}

func TestRecordOverwrite(t *testing.T) {
	r := NewRecorder("/tmp/dumps")
	r.Record(&TurnDump{SessionID: "sess_1", Input: "first"})
	r.Record(&TurnDump{SessionID: "sess_1", Input: "second"})

	got := r.Last("sess_1")
	if got == nil || got.Input != "second" {
		t.Errorf("expected 'second', got %v", got)
	}
}

func TestRecordMultipleSessions(t *testing.T) {
	r := NewRecorder("/tmp/dumps")
	r.Record(&TurnDump{SessionID: "sess_a", Input: "a"})
	r.Record(&TurnDump{SessionID: "sess_b", Input: "b"})

	if got := r.Last("sess_a"); got == nil || got.Input != "a" {
		t.Errorf("expected 'a', got %v", got)
	}
	if got := r.Last("sess_b"); got == nil || got.Input != "b" {
		t.Errorf("expected 'b', got %v", got)
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir)

	r.Record(&TurnDump{
		SessionID:    "sess_write",
		ModelName:    "test-model",
		Input:        "test input",
		SystemPrompt: "system prompt",
		Messages: []types.Message{
			{Role: "user", Parts: []types.ContentPart{types.TextPart("hello")}},
		},
		ToolResults: []types.ToolResult{
			{Content: "result"},
		},
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	path, err := r.Write("sess_write")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedPath := filepath.Join(dir, "sess_write.json")
	if path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file")
	}
}

func TestWriteNoDump(t *testing.T) {
	r := NewRecorder("/tmp/dumps")
	_, err := r.Write("nonexistent")
	if err == nil {
		t.Fatal("expected error writing nonexistent dump")
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dumps")
	r := NewRecorder(dir)
	r.Record(&TurnDump{SessionID: "sess_mkdir", Input: "x"})

	path, err := r.Write("sess_mkdir")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to exist after Write")
	}
}

func TestRecorderDir(t *testing.T) {
	r := NewRecorder("/custom/dir")
	if got := r.Dir(); got != "/custom/dir" {
		t.Errorf("expected '/custom/dir', got %q", got)
	}
}

func TestWrite_MkdirAllFails(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	r := NewRecorder(f.Name() + "/subdir")
	r.Record(&TurnDump{SessionID: "sess_fail", Input: "x"})
	_, err = r.Write("sess_fail")
	if err == nil {
		t.Fatal("expected error when MkdirAll fails")
	}
}

func TestConcurrentRecordAndLast(t *testing.T) {
	r := NewRecorder("/tmp/dumps")
	done := make(chan struct{})
	go func() {
		r.Record(&TurnDump{SessionID: "concurrent", Input: "from goroutine"})
		close(done)
	}()
	r.Record(&TurnDump{SessionID: "concurrent", Input: "from main"})
	<-done
	// Should not panic — just check last value
	_ = r.Last("concurrent")
}
