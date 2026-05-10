package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphinzZ/internal/config"
)

func TestNewManager(t *testing.T) {
	cfg := config.CrontabConfig{
		File:          filepath.Join(t.TempDir(), "CRONTAB.md"),
		CheckInterval: "10s",
	}
	m := NewManager(cfg)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.checkInterval != 10*time.Second {
		t.Errorf("expected 10s interval, got %v", m.checkInterval)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// File should have been created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
	if len(m.List()) != 0 {
		t.Error("expected empty task list")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	os.WriteFile(path, []byte(""), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
}

func TestParseValidEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	content := `# Header
---
name: test-task
schedule: "*/5 * * * *"
description: A test task
enabled: true
---

Run the test command.

---
`
	os.WriteFile(path, []byte(content), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "test-task" {
		t.Errorf("expected name 'test-task', got %q", tasks[0].Name)
	}
	if tasks[0].Schedule != "*/5 * * * *" {
		t.Errorf("expected schedule '*/5 * * * *', got %q", tasks[0].Schedule)
	}
	if !tasks[0].Enabled {
		t.Error("expected enabled true")
	}
	if !strings.Contains(tasks[0].Task, "Run the test command") {
		t.Errorf("expected task body, got %q", tasks[0].Task)
	}
}

func TestParseMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	content := `# CRONTAB
---
name: task-a
schedule: "0 9 * * *"
description: Morning task
enabled: true
---

Do morning thing.

---
---
name: task-b
schedule: "0 18 * * *"
description: Evening task
enabled: true
---

Do evening thing.

---
`
	os.WriteFile(path, []byte(content), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	tasks := m.List()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestInvalidCronExpression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	content := `---
name: bad-cron
schedule: "invalid"
description: Bad cron
enabled: true
---

Do something.

---
`
	os.WriteFile(path, []byte(content), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Enabled {
		t.Error("expected task with invalid cron to be disabled")
	}
}

func TestCorruptedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	os.WriteFile(path, []byte("total garbage\n---\nbroken yaml\n---\nno name field"), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	tasks := m.List()
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for corrupted content, got %d", len(tasks))
	}
	// Original file should be preserved (not deleted)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("crrontab file should not be deleted")
	}
}

func TestAddTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	task := &CronTask{
		Name:        "test",
		Schedule:    "0 18 * * *",
		Description: "Test task",
		Enabled:     true,
		Task:        "Run the test.",
	}
	if err := m.AddTask(task); err != nil {
		t.Fatalf("AddTask error: %v", err)
	}

	tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "test" {
		t.Errorf("expected name 'test', got %q", tasks[0].Name)
	}
}

func TestAddTaskDuplicateName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{Name: "test", Schedule: "0 18 * * *", Task: "x"})
	err := m.AddTask(&CronTask{Name: "test", Schedule: "0 9 * * *", Task: "y"})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestAddTaskInvalidCron(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	err := m.AddTask(&CronTask{Name: "test", Schedule: "bad", Task: "x"})
	if err == nil {
		t.Fatal("expected error for invalid cron")
	}
}

func TestRemoveTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{Name: "a", Schedule: "0 18 * * *", Task: "a"})
	m.AddTask(&CronTask{Name: "b", Schedule: "0 9 * * *", Task: "b"})

	if !m.RemoveTask("a") {
		t.Fatal("RemoveTask returned false")
	}
	tasks := m.List()
	if len(tasks) != 1 || tasks[0].Name != "b" {
		t.Errorf("expected only 'b', got %v", tasks)
	}

	if m.RemoveTask("nonexistent") {
		t.Error("RemoveTask should return false for non-existent name")
	}
}

func TestRemoveTaskFilePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{Name: "a", Schedule: "0 18 * * *", Task: "task a"})
	m.AddTask(&CronTask{Name: "b", Schedule: "0 9 * * *", Task: "task b"})
	m.RemoveTask("a")

	// Reload from file
	m2 := NewManager(config.CrontabConfig{File: path})
	m2.Load()
	tasks := m2.List()
	if len(tasks) != 1 || tasks[0].Name != "b" {
		t.Errorf("expected only 'b' after reload, got %v", tasks)
	}
}

func TestToggleTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{Name: "test", Schedule: "0 18 * * *", Enabled: true, Task: "x"})

	if !m.ToggleTask("test", false) {
		t.Fatal("ToggleTask returned false")
	}
	task, ok := m.Get("test")
	if !ok {
		t.Fatal("Get returned false")
	}
	if task.Enabled {
		t.Error("expected task to be disabled")
	}

	// Reload and verify persisted
	m2 := NewManager(config.CrontabConfig{File: path})
	m2.Load()
	task2, _ := m2.Get("test")
	if task2.Enabled {
		t.Error("expected disabled after reload")
	}
}

func TestGetNonExistent(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(config.CrontabConfig{File: filepath.Join(dir, "CRONTAB.md")})
	m.Load()

	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected false for non-existent task")
	}
}

func TestAddResult(t *testing.T) {
	m := NewManager(config.CrontabConfig{File: filepath.Join(t.TempDir(), "CRONTAB.md")})
	m.AddResult("test", true, "done", "")
	m.AddResult("test2", false, "", "error msg")

	results := m.PendingResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].TaskName != "test" || !results[0].Success || results[0].Output != "done" {
		t.Errorf("unexpected result: %+v", results[0])
	}
	if results[1].TaskName != "test2" || results[1].Success || results[1].Error != "error msg" {
		t.Errorf("unexpected result: %+v", results[1])
	}

	// Second call should be empty
	if r := m.PendingResults(); r != nil {
		t.Error("expected nil after drain")
	}
}

func TestAddResultMaxSize(t *testing.T) {
	m := NewManager(config.CrontabConfig{File: filepath.Join(t.TempDir(), "CRONTAB.md")})
	for i := 0; i < 150; i++ {
		m.AddResult("test", true, "", "")
	}

	results := m.PendingResults()
	if len(results) != 100 {
		t.Errorf("expected 100 results (capped), got %d", len(results))
	}
}

func TestCheckDue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	// A task scheduled every minute, enabled
	m.AddTask(&CronTask{
		Name:     "every-min",
		Schedule: "*/1 * * * *",
		Enabled:  true,
		Task:     "do it",
	})

	// Check due — should fire since lastRun is zero time
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dueCh := m.Start(ctx)

	select {
	case task := <-dueCh:
		if task.Name != "every-min" {
			t.Errorf("expected every-min, got %s", task.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for due task")
	}
}

func TestDueChBuffer(t *testing.T) {
	// Test that dueCh doesn't block when multiple tasks are due
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{
		File:          path,
		CheckInterval: "1h", // won't tick in test
	})
	m.Load()

	m.tasks = append(m.tasks, &CronTask{
		Name:     "t1",
		Schedule: "*/1 * * * *",
		Enabled:  true,
		Task:     "x",
	})
	m.tasks = append(m.tasks, &CronTask{
		Name:     "t2",
		Schedule: "*/1 * * * *",
		Enabled:  true,
		Task:     "x",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m.Start(ctx)

	// Both tasks should be due immediately (zero lastRun)
	time.Sleep(100 * time.Millisecond)

	// Drain channel
	var got int
	for i := 0; i < 2; i++ {
		select {
		case <-m.dueCh:
			got++
		case <-time.After(time.Second):
			break
		}
	}
	if got != 2 {
		t.Errorf("expected 2 due tasks, got %d", got)
	}
}

func TestDisabledTaskNotDue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{
		Name:     "disabled",
		Schedule: "*/1 * * * *",
		Enabled:  false,
		Task:     "x",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m.Start(ctx)

	// Should NOT fire for disabled task
	time.Sleep(200 * time.Millisecond)
	select {
	case task := <-m.dueCh:
		t.Errorf("unexpected due task for disabled task: %s", task.Name)
	default:
		// OK
	}
}

func TestStop(t *testing.T) {
	m := NewManager(config.CrontabConfig{File: filepath.Join(t.TempDir(), "CRONTAB.md")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel
	_ = m.Start(ctx)

	// Should stop without panic
	time.Sleep(100 * time.Millisecond)
}

func TestRewritePreservesTasks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	m := NewManager(config.CrontabConfig{File: path})
	m.Load()

	m.AddTask(&CronTask{Name: "a", Schedule: "0 18 * * *", Description: "desc a", Enabled: true, Task: "do a"})
	m.AddTask(&CronTask{Name: "b", Schedule: "0 9 * * *", Description: "desc b", Enabled: false, Task: "do b"})

	// Remove and add to trigger internal rewrite
	m.RemoveTask("a")
	m.AddTask(&CronTask{Name: "c", Schedule: "*/5 * * * *", Description: "desc c", Enabled: true, Task: "do c"})

	// Reload
	m2 := NewManager(config.CrontabConfig{File: path})
	m2.Load()
	tasks := m2.List()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	// Expect b and c (a was removed)
	names := map[string]bool{}
	for _, t := range tasks {
		names[t.Name] = true
	}
	if !names["b"] || !names["c"] {
		t.Errorf("expected tasks b and c, got %v", tasks)
	}
}

func TestLoadSkipsBodyWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CRONTAB.md")
	content := `# Just some markdown text

Not a valid entry without frontmatter.

---
`
	os.WriteFile(path, []byte(content), 0644)
	m := NewManager(config.CrontabConfig{File: path})
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(m.List()) != 0 {
		t.Error("expected 0 tasks for non-frontmatter content")
	}
}

func TestDefaultInterval(t *testing.T) {
	m := NewManager(config.CrontabConfig{File: filepath.Join(t.TempDir(), "CRONTAB.md")})
	if m.checkInterval != 30*time.Second {
		t.Errorf("expected default 30s, got %v", m.checkInterval)
	}
}
