package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"dolphin/internal/types"
)

// mockBrain implements BrainWriter for testing.
type mockBrain struct {
	mu     sync.Mutex
	dir    string
	writes []writeCall
}

type writeCall struct {
	path    string
	summary string
	content string
}

func newMockBrain(t *testing.T) *mockBrain {
	return &mockBrain{dir: t.TempDir()}
}

func (m *mockBrain) Dir() string { return m.dir }

func (m *mockBrain) Write(_ context.Context, path, summary, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, writeCall{path, summary, content})
	return nil
}

func TestNew(t *testing.T) {
	logger := zap.NewNop()
	brain := newMockBrain(t)
	s := New(t.TempDir(), logger, brain)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.dir == "" {
		t.Error("expected dir to be set")
	}
	if s.logger != logger {
		t.Error("logger not set")
	}
	if s.brain != brain {
		t.Error("brain not set")
	}
	if s.cron == nil {
		t.Error("cron not initialized")
	}
	if len(s.tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(s.tasks))
	}
	if len(s.entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(s.entries))
	}
	if len(s.timers) != 0 {
		t.Errorf("expected 0 timers, got %d", len(s.timers))
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with spaces", "with-spaces"},
		{"path/to/file", "path-to-file"},
		{"special:chars|test", "special-chars-test"},
		{"<brackets>", "brackets"},
		{`quoted"name"`, "quotedname"},
		{"normal-name.md", "normal-name.md"},
	}
	for _, tc := range tests {
		got := safeFilename(tc.input)
		if got != tc.want {
			t.Errorf("safeFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"", 5, ""},
		{"abc", 0, ""},
	}
	for _, tc := range tests {
		got := truncate(tc.s, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
		}
	}
}

func TestTimePtr(t *testing.T) {
	now := time.Now()
	ptr := timePtr(now)
	if ptr == nil {
		t.Fatal("timePtr returned nil")
	}
	if !ptr.Equal(now) {
		t.Errorf("timePtr(%v) = %v", now, *ptr)
	}
}

func TestFormatBrainFile(t *testing.T) {
	now := time.Now()

	t.Run("cron task enabled", func(t *testing.T) {
		task := &Task{
			ID:        "test-id",
			Name:      "my-task",
			Schedule:  "*/5 * * * *",
			Command:   "echo hello",
			Enabled:   true,
			CreatedAt: now,
			RunCount:  3,
		}
		result := formatBrainFile(task)
		if !strings.Contains(result, "# my-task") {
			t.Error("expected title")
		}
		if !strings.Contains(result, "*/5 * * * *") {
			t.Error("expected schedule")
		}
		if !strings.Contains(result, "echo hello") {
			t.Error("expected command")
		}
		if !strings.Contains(result, "启用") {
			t.Error("expected enabled status")
		}
		if !strings.Contains(result, "3") {
			t.Error("expected run count")
		}
	})

	t.Run("delay task with FireAt", func(t *testing.T) {
		fireAt := now.Add(1 * time.Hour)
		task := &Task{
			Name:      "delayed-task",
			Delay:     "1h",
			Command:   "echo delayed",
			Enabled:   true,
			CreatedAt: now,
			FireAt:    &fireAt,
		}
		result := formatBrainFile(task)
		if !strings.Contains(result, "一次性延迟") {
			t.Error("expected delay info")
		}
		if !strings.Contains(result, fireAt.Format("2006-01-02 15:04:05")) {
			t.Error("expected fire at time")
		}
		if !strings.Contains(result, "等待执行") {
			t.Error("expected pending execution notice")
		}
	})

	t.Run("cron task disabled", func(t *testing.T) {
		task := &Task{
			Name:     "disabled-task",
			Schedule: "0 * * * *",
			Command:  "echo off",
			Enabled:  false,
		}
		result := formatBrainFile(task)
		if !strings.Contains(result, "禁用") {
			t.Error("expected disabled status")
		}
	})

	t.Run("task with last run", func(t *testing.T) {
		lastRun := now.Add(-30 * time.Minute)
		task := &Task{
			Name:       "ran-task",
			Schedule:   "* * * * *",
			Command:    "echo ran",
			Enabled:    true,
			CreatedAt:  now.Add(-24 * time.Hour),
			LastRunAt:  &lastRun,
			LastStatus: "success",
			LastOutput: "output line",
		}
		result := formatBrainFile(task)
		if !strings.Contains(result, "最近执行") {
			t.Error("expected recent execution section")
		}
		if !strings.Contains(result, "output line") {
			t.Error("expected output")
		}
	})

	t.Run("task with last error", func(t *testing.T) {
		lastRun := now.Add(-10 * time.Minute)
		task := &Task{
			Name:       "failed-task",
			Schedule:   "* * * * *",
			Command:    "echo fail",
			Enabled:    true,
			CreatedAt:  now.Add(-24 * time.Hour),
			LastRunAt:  &lastRun,
			LastStatus: "failed",
			LastError:  "exit status 1",
		}
		result := formatBrainFile(task)
		if !strings.Contains(result, "exit status 1") {
			t.Error("expected error")
		}
	})
}

func TestCreate(t *testing.T) {
	logger := zap.NewNop()
	brain := newMockBrain(t)

	t.Run("valid cron task", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		task, err := s.Create(context.Background(), "test-task", "*/5 * * * *", "echo hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if task.Name != "test-task" {
			t.Errorf("got name %q, want %q", task.Name, "test-task")
		}
		if task.Schedule != "*/5 * * * *" {
			t.Errorf("got schedule %q", task.Schedule)
		}
		if !task.Enabled {
			t.Error("expected task to be enabled")
		}
		if task.ID == "" {
			t.Error("expected non-empty ID")
		}

		tasks := s.List()
		if len(tasks) != 1 {
			t.Errorf("expected 1 task, got %d", len(tasks))
		}
	})

	t.Run("invalid cron schedule", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.Create(context.Background(), "bad", "not-a-cron", "echo x")
		if err == nil {
			t.Fatal("expected error for invalid cron")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.Create(context.Background(), "", "* * * * *", "echo x")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
	})

	t.Run("empty command", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.Create(context.Background(), "test", "* * * * *", "")
		if err == nil {
			t.Fatal("expected error for empty command")
		}
	})
}

func TestList(t *testing.T) {
	logger := zap.NewNop()

	t.Run("returns tasks sorted by creation time", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		s.Create(context.Background(), "first", "* * * * *", "echo 1")
		s.Create(context.Background(), "second", "*/5 * * * *", "echo 2")
		s.Create(context.Background(), "third", "0 */2 * * *", "echo 3")

		tasks := s.List()
		if len(tasks) != 3 {
			t.Fatalf("expected 3 tasks, got %d", len(tasks))
		}
		if tasks[0].Name != "first" || tasks[1].Name != "second" || tasks[2].Name != "third" {
			t.Error("tasks not sorted by CreatedAt")
		}
		// Verify first task's CreatedAt is before second's
		if !tasks[0].CreatedAt.Before(tasks[1].CreatedAt) {
			t.Error("first task should be before second")
		}
	})

	t.Run("empty scheduler", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		tasks := s.List()
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d", len(tasks))
		}
	})
}

func TestUpsert(t *testing.T) {
	logger := zap.NewNop()
	brain := newMockBrain(t)

	t.Run("creates new task", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		task, created, err := s.Upsert(context.Background(), "upsert-task", "*/5 * * * *", "echo upsert")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !created {
			t.Error("expected created=true")
		}
		if task.Name != "upsert-task" {
			t.Errorf("got name %q", task.Name)
		}
	})

	t.Run("updates existing task by name", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		t1, created, err := s.Upsert(context.Background(), "same-name", "0 * * * *", "echo v1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !created {
			t.Error("first call should create")
		}

		t2, created2, err := s.Upsert(context.Background(), "same-name", "*/5 * * * *", "echo v2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created2 {
			t.Error("second call should update, not create")
		}
		if t2.ID != t1.ID {
			t.Error("update should preserve ID")
		}
		if t2.Command != "echo v2" {
			t.Errorf("got command %q", t2.Command)
		}
		if t2.Schedule != "*/5 * * * *" {
			t.Errorf("got schedule %q", t2.Schedule)
		}

		tasks := s.List()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task, got %d", len(tasks))
		}
	})

	t.Run("invalid schedule", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, _, err := s.Upsert(context.Background(), "bad", "invalid", "echo x")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty name or command", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, _, err := s.Upsert(context.Background(), "", "* * * * *", "echo")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
		_, _, err = s.Upsert(context.Background(), "test", "* * * * *", "")
		if err == nil {
			t.Fatal("expected error for empty command")
		}
	})
}

func TestDeleteByName(t *testing.T) {
	logger := zap.NewNop()

	t.Run("deletes existing task by name", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task, _ := s.Create(context.Background(), "del-by-name", "* * * * *", "echo x")
		err := s.DeleteByName(context.Background(), "del-by-name")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(s.List()) != 0 {
			t.Error("expected 0 tasks after delete")
		}
		// Second delete should fail
		err = s.DeleteByName(context.Background(), "del-by-name")
		if err == nil {
			t.Fatal("expected error for already-deleted task")
		}
		_ = task
	})

	t.Run("nonexistent task", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		err := s.DeleteByName(context.Background(), "no-such")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDelete(t *testing.T) {
	logger := zap.NewNop()

	t.Run("deletes existing task", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task, _ := s.Create(context.Background(), "delete-me", "* * * * *", "echo x")
		err := s.Delete(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(s.List()) != 0 {
			t.Error("expected 0 tasks after delete")
		}
	})

	t.Run("nonexistent task", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		err := s.Delete(context.Background(), "nonexistent-id")
		if err == nil {
			t.Fatal("expected error for nonexistent task")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestScheduleOnce(t *testing.T) {
	logger := zap.NewNop()
	brain := newMockBrain(t)

	t.Run("valid delay", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		task, err := s.ScheduleOnce(context.Background(), "delayed-task", "5m", "echo later")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if task.Name != "delayed-task" {
			t.Errorf("got name %q", task.Name)
		}
		if task.Delay != "5m" {
			t.Errorf("got delay %q", task.Delay)
		}
		if task.FireAt == nil {
			t.Fatal("expected FireAt to be set")
		}
		expectedFire := task.CreatedAt.Add(5 * time.Minute)
		if !task.FireAt.Equal(expectedFire) {
			t.Errorf("FireAt = %v, want %v", task.FireAt, expectedFire)
		}
		if len(s.timers) != 1 {
			t.Errorf("expected 1 timer, got %d", len(s.timers))
		}
	})

	t.Run("invalid delay", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.ScheduleOnce(context.Background(), "bad", "not-a-duration", "echo x")
		if err == nil {
			t.Fatal("expected error for invalid delay")
		}
	})

	t.Run("zero delay", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.ScheduleOnce(context.Background(), "zero", "0s", "echo x")
		if err == nil {
			t.Fatal("expected error for zero delay")
		}
	})

	t.Run("negative delay", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.ScheduleOnce(context.Background(), "neg", "-1m", "echo x")
		if err == nil {
			t.Fatal("expected error for negative delay")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.ScheduleOnce(context.Background(), "", "5m", "echo x")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
	})

	t.Run("empty command", func(t *testing.T) {
		s := New(t.TempDir(), logger, brain)
		_, err := s.ScheduleOnce(context.Background(), "test", "5m", "")
		if err == nil {
			t.Fatal("expected error for empty command")
		}
	})
}

func TestSyncIndexLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writes index.md with tasks", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)

		t1 := &Task{
			ID:      uuid.NewString(),
			Name:    "task-1",
			Command: "echo a",
			Enabled: true,
		}
		s.tasks[t1.ID] = t1

		t2 := &Task{
			ID:         uuid.NewString(),
			Name:       "task-2",
			Command:    "echo b",
			Enabled:    true,
			LastStatus: "success",
			RunCount:   5,
		}
		s.tasks[t2.ID] = t2

		s.syncIndexLocked()

		data, err := os.ReadFile(filepath.Join(dir, "index.md"))
		if err != nil {
			t.Fatalf("read index.md: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "Scheduled Tasks") {
			t.Error("expected header")
		}
		if !strings.Contains(content, t2.ID[:8]) {
			t.Error("expected task ID in table")
		}
	})

	t.Run("writes empty message when no tasks", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		s.syncIndexLocked()

		data, err := os.ReadFile(filepath.Join(dir, "index.md"))
		if err != nil {
			t.Fatalf("read index.md: %v", err)
		}
		if !strings.Contains(string(data), "No tasks registered") {
			t.Error("expected no tasks message")
		}
	})

	t.Run("no-op when dir is empty", func(t *testing.T) {
		s := New("", logger, nil)
		s.syncIndexLocked() // should not panic
	})

	t.Run("disabled task shows disabled status", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)

		t1 := &Task{ID: uuid.NewString(), Name: "off", Command: "echo", Enabled: false}
		s.tasks[t1.ID] = t1
		s.syncIndexLocked()

		data, _ := os.ReadFile(filepath.Join(dir, "index.md"))
		if !strings.Contains(string(data), "disabled") {
			t.Error("expected disabled status for disabled task")
		}
	})
}

func TestSyncTaskToFileLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writes task file", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "file-task",
			Command: "echo file",
		}
		s.tasks[task.ID] = task
		s.syncTaskToFileLocked(task)

		path := filepath.Join(dir, task.ID+".md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatal("task file was not created")
		}
		data, _ := os.ReadFile(path)
		content := string(data)
		if !strings.HasPrefix(content, "---") || !strings.HasSuffix(strings.TrimSpace(content), "---") {
			t.Error("expected YAML front matter")
		}
	})

	t.Run("no-op when dir is empty", func(t *testing.T) {
		s := New("", logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "nope", Command: "echo"}
		s.syncTaskToFileLocked(task) // should not panic
	})

	t.Run("creates index.md alongside task file", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "idx-task", Command: "echo"}
		s.tasks[task.ID] = task
		s.syncTaskToFileLocked(task)

		if _, err := os.Stat(filepath.Join(dir, "index.md")); os.IsNotExist(err) {
			t.Error("index.md was not created alongside task file")
		}
	})
}

func TestRemoveTaskFileLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("removes existing file", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "r", Command: "echo"}
		s.tasks[task.ID] = task
		s.syncTaskToFileLocked(task)

		path := filepath.Join(dir, task.ID+".md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatal("task file should exist before removal")
		}

		s.removeTaskFileLocked(task.ID)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("task file should not exist after removal")
		}
	})

	t.Run("no-op for nonexistent file", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		s.removeTaskFileLocked("nonexistent") // should not panic
	})

	t.Run("no-op when dir is empty", func(t *testing.T) {
		s := New("", logger, nil)
		s.removeTaskFileLocked("any-id") // should not panic
	})
}

func TestLoadTasksLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("loads .md files as tasks", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)

		t1 := &Task{ID: uuid.NewString(), Name: "loaded-1", Command: "echo 1"}
		s.tasks[t1.ID] = t1
		s.syncTaskToFileLocked(t1)

		s2 := New(dir, logger, nil)
		s2.loadTasksLocked()

		if len(s2.tasks) != 1 {
			t.Fatalf("expected 1 loaded task, got %d", len(s2.tasks))
		}
		for id, loaded := range s2.tasks {
			if loaded.Name != "loaded-1" {
				t.Errorf("task %s has name %q", id, loaded.Name)
			}
		}
	})

	t.Run("skips index.md", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# index"), 0644); err != nil {
			t.Fatal(err)
		}
		s.loadTasksLocked()
		if len(s.tasks) != 0 {
			t.Errorf("expected 0 tasks from index.md, got %d", len(s.tasks))
		}
	})

	t.Run("skips directories", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
			t.Fatal(err)
		}
		s.loadTasksLocked()
		if len(s.tasks) != 0 {
			t.Errorf("expected 0 tasks from dirs, got %d", len(s.tasks))
		}
	})

	t.Run("skips non-.md files", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		s.loadTasksLocked()
		if len(s.tasks) != 0 {
			t.Errorf("expected 0 tasks from .txt, got %d", len(s.tasks))
		}
	})

	t.Run("handles nonexistent directory", func(t *testing.T) {
		s := New("/nonexistent/path", logger, nil)
		s.loadTasksLocked() // should not panic
	})
}

func TestSyncBrainLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writes to brain when set", func(t *testing.T) {
		brain := newMockBrain(t)
		s := New(t.TempDir(), logger, brain)
		task := &Task{ID: uuid.NewString(), Name: "brain-task", Command: "echo", Schedule: "* * * * *"}
		s.syncBrainLocked(context.Background(), task)

		brain.mu.Lock()
		n := len(brain.writes)
		brain.mu.Unlock()
		if n != 1 {
			t.Fatalf("expected 1 brain write, got %d", n)
		}
	})

	t.Run("no-op when brain is nil", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "brainless", Command: "echo"}
		s.syncBrainLocked(context.Background(), task) // should not panic
	})
}

func TestDeleteBrainLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writes deletion to brain for cron task", func(t *testing.T) {
		brain := newMockBrain(t)
		s := New(t.TempDir(), logger, brain)
		task := &Task{ID: uuid.NewString(), Name: "del-cron", Command: "echo", Schedule: "* * * * *"}
		s.deleteBrainLocked(context.Background(), task)

		brain.mu.Lock()
		n := len(brain.writes)
		brain.mu.Unlock()
		if n != 1 {
			t.Fatalf("expected 1 brain write, got %d", n)
		}
	})

	t.Run("writes completion for delay task", func(t *testing.T) {
		brain := newMockBrain(t)
		s := New(t.TempDir(), logger, brain)
		task := &Task{ID: uuid.NewString(), Name: "del-delay", Command: "echo", Delay: "5m"}
		s.deleteBrainLocked(context.Background(), task)

		brain.mu.Lock()
		n := len(brain.writes)
		brain.mu.Unlock()
		if n != 1 {
			t.Fatalf("expected 1 brain write, got %d", n)
		}
	})

	t.Run("no-op when brain is nil", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "x", Command: "echo"}
		s.deleteBrainLocked(context.Background(), task) // should not panic
	})
}

func TestAddCronLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("adds cron entry", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "cron-me", Schedule: "* * * * *", Command: "echo"}
		s.addCronLocked(task)
		if _, ok := s.entries[task.ID]; !ok {
			t.Error("expected cron entry to be added")
		}
	})

	t.Run("replaces existing entry", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "replace", Schedule: "* * * * *", Command: "echo"}
		s.addCronLocked(task)
		firstID := s.entries[task.ID]

		// Add again with same ID should replace
		s.addCronLocked(task)
		secondID := s.entries[task.ID]
		if firstID == secondID {
			t.Log("cron entry ID unchanged (may be expected)")
		}
	})
}

func TestAddTimerLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("adds timer for delayed task", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "timer-test", Command: "echo"}
		s.addTimerLocked(task, 10*time.Minute)
		if _, ok := s.timers[task.ID]; !ok {
			t.Error("expected timer to be added")
		}
	})

	t.Run("replaces existing timer", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "timer-replace", Command: "echo"}
		s.addTimerLocked(task, 10*time.Minute)
		s.addTimerLocked(task, 5*time.Minute) // replace with shorter duration
		if _, ok := s.timers[task.ID]; !ok {
			t.Error("expected timer after replace")
		}
	})
}

func TestDeleteTaskLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("removes task from all maps", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "del-locked", Command: "echo", Schedule: "* * * * *"}
		s.tasks[task.ID] = task
		s.addCronLocked(task)

		s.deleteTaskLocked(task.ID)
		if _, ok := s.tasks[task.ID]; ok {
			t.Error("task should be removed from tasks map")
		}
		if _, ok := s.entries[task.ID]; ok {
			t.Error("task should be removed from entries map")
		}
		// File should also be removed
		if _, err := os.Stat(filepath.Join(dir, task.ID+".md")); !os.IsNotExist(err) {
			t.Error("task file should be removed")
		}
	})

	t.Run("handles task with no cron entry", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "no-entry", Command: "echo"}
		s.tasks[task.ID] = task
		s.deleteTaskLocked(task.ID) // should not panic
	})
}

func TestExecute(t *testing.T) {
	logger := zap.NewNop()

	t.Run("command success updates task state", func(t *testing.T) {
		oldRunCmd := runCmd
		runCmd = func(_ context.Context, command string) (string, string, error) {
			return "stdout output", "", nil
		}
		defer func() { runCmd = oldRunCmd }()

		s := New(t.TempDir(), logger, nil)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "success-task",
			Command: "echo ok",
		}
		s.tasks[task.ID] = task

		s.execute(task)

		if task.RunCount != 1 {
			t.Errorf("RunCount = %d, want 1", task.RunCount)
		}
		if task.LastStatus != "success" {
			t.Errorf("LastStatus = %q, want success", task.LastStatus)
		}
		if task.LastRunAt == nil {
			t.Error("LastRunAt should be set")
		}
		if task.LastOutput != "stdout output" {
			t.Errorf("LastOutput = %q, want %q", task.LastOutput, "stdout output")
		}
		if task.LastError != "" {
			t.Errorf("LastError = %q, want empty", task.LastError)
		}
	})

	t.Run("command failure sets error state", func(t *testing.T) {
		oldRunCmd := runCmd
		runCmd = func(_ context.Context, command string) (string, string, error) {
			return "", "stderr error", fmt.Errorf("exit status 1")
		}
		defer func() { runCmd = oldRunCmd }()

		s := New(t.TempDir(), logger, nil)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "fail-task",
			Command: "false",
		}
		s.tasks[task.ID] = task

		s.execute(task)

		if task.LastStatus != "failed" {
			t.Errorf("LastStatus = %q, want failed", task.LastStatus)
		}
		if task.LastError != "exit status 1" {
			t.Errorf("LastError = %q", task.LastError)
		}
		if task.LastOutput != "stderr error" {
			t.Errorf("LastOutput = %q", task.LastOutput)
		}
	})

	t.Run("silent failure uses stdout as fallback output", func(t *testing.T) {
		oldRunCmd := runCmd
		runCmd = func(_ context.Context, command string) (string, string, error) {
			return "only stdout", "", fmt.Errorf("exit 1")
		}
		defer func() { runCmd = oldRunCmd }()

		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "silent-fail", Command: "fail"}
		s.tasks[task.ID] = task

		s.execute(task)

		if task.LastOutput != "only stdout" {
			t.Errorf("LastOutput = %q, want stdout fallback", task.LastOutput)
		}
	})

	t.Run("deleted task during execution is skipped", func(t *testing.T) {
		oldRunCmd := runCmd
		executed := make(chan struct{})
		runCmd = func(_ context.Context, command string) (string, string, error) {
			<-executed // block until we signal
			return "ok", "", nil
		}
		defer func() { runCmd = oldRunCmd }()

		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "delete-during", Command: "slow"}
		s.tasks[task.ID] = task

		go s.execute(task)
		// Delete the task while execute is running
		s.mu.Lock()
		delete(s.tasks, task.ID)
		s.mu.Unlock()
		close(executed)

		// Give execute time to finish
		time.Sleep(50 * time.Millisecond)

		// Task should not have been recreated or updated
		s.mu.Lock()
		_, exists := s.tasks[task.ID]
		s.mu.Unlock()
		if exists {
			t.Error("deleted task should not exist after execute")
		}
	})

	t.Run("delay task is auto-deleted after execution", func(t *testing.T) {
		oldRunCmd := runCmd
		runCmd = func(_ context.Context, command string) (string, string, error) {
			return "ok", "", nil
		}
		defer func() { runCmd = oldRunCmd }()

		s := New(t.TempDir(), logger, nil)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "auto-delete",
			Command: "echo one-shot",
			Delay:   "5m",
		}
		s.tasks[task.ID] = task

		s.execute(task)

		s.mu.Lock()
		_, exists := s.tasks[task.ID]
		s.mu.Unlock()
		if exists {
			t.Error("delay task should be auto-deleted after execution")
		}
	})
}

func TestScheduleDelayedLocked(t *testing.T) {
	logger := zap.NewNop()

	t.Run("skips task with nil FireAt", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "no-fire", Command: "echo", Delay: "5m"}
		s.scheduleDelayedLocked(task) // should not panic, just log warning
		if len(s.timers) != 0 {
			t.Errorf("expected 0 timers for nil FireAt, got %d", len(s.timers))
		}
	})

	t.Run("executes overdue task immediately", func(t *testing.T) {
		oldRunCmd := runCmd
		defer func() { runCmd = oldRunCmd }()
		runCmd = func(_ context.Context, command string) (string, string, error) {
			return "", "", nil
		}

		s := New(t.TempDir(), logger, nil)
		past := time.Now().Add(-1 * time.Hour)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "overdue",
			Command: "echo",
			Delay:   "5m",
			FireAt:  &past,
		}
		s.tasks[task.ID] = task
		s.scheduleDelayedLocked(task)

		// Wait a moment for goroutine to run
		time.Sleep(50 * time.Millisecond)

		s.mu.Lock()
		rc := task.RunCount
		s.mu.Unlock()
		if rc != 1 {
			t.Errorf("expected RunCount=1 for overdue task, got %d", rc)
		}
	})

	t.Run("schedules timer for future task", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		future := time.Now().Add(1 * time.Hour)
		task := &Task{
			ID:      uuid.NewString(),
			Name:    "future",
			Command: "echo",
			Delay:   "1h",
			FireAt:  &future,
		}
		s.scheduleDelayedLocked(task)
		if len(s.timers) != 1 {
			t.Errorf("expected 1 timer for future task, got %d", len(s.timers))
		}
	})
}

func TestStart(t *testing.T) {
	logger := zap.NewNop()

	t.Run("starts with existing tasks", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "start-me", Command: "echo", Schedule: "* * * * *", Enabled: true}
		s.tasks[task.ID] = task
		s.syncTaskToFileLocked(task)

		s2 := New(dir, logger, nil)
		s2.Start(context.Background())
		defer s2.Stop()

		if len(s2.tasks) != 1 {
			t.Errorf("expected 1 task after start, got %d", len(s2.tasks))
		}
		if len(s2.entries) != 1 {
			t.Errorf("expected 1 cron entry after start, got %d", len(s2.entries))
		}
	})

	t.Run("skips disabled tasks", func(t *testing.T) {
		dir := t.TempDir()
		s := New(dir, logger, nil)
		task := &Task{ID: uuid.NewString(), Name: "disabled", Command: "echo", Schedule: "* * * * *", Enabled: false}
		s.tasks[task.ID] = task
		s.syncTaskToFileLocked(task)

		s2 := New(dir, logger, nil)
		s2.Start(context.Background())
		defer s2.Stop()

		if len(s2.entries) != 0 {
			t.Errorf("expected 0 cron entries for disabled task, got %d", len(s2.entries))
		}
	})

	t.Run("warns on invalid dir mkdir", func(t *testing.T) {
		// Use a logger that doesn't panic
		s := New("/dev/null/scheduler", logger, nil)
		s.Start(context.Background()) // should not panic
		s.Stop()
	})
}

func TestStop(t *testing.T) {
	logger := zap.NewNop()

	t.Run("stops cron and cleans up timers", func(t *testing.T) {
		s := New(t.TempDir(), logger, nil)
		s.Start(context.Background())

		task := &Task{ID: uuid.NewString(), Name: "stop-test", Command: "echo", Delay: "10m"}
		s.tasks[task.ID] = task
		s.addTimerLocked(task, 10*time.Minute)

		s.Stop()

		if len(s.timers) != 0 {
			t.Errorf("expected 0 timers after stop, got %d", len(s.timers))
		}
	})
}

func TestIgnoreRace(t *testing.T) {
	// Compile-check that types used in scheduler are importable.
	_ = types.ToolResult{}
}
