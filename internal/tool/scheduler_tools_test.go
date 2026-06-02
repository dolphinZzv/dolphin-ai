package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/scheduler"
	"dolphin/internal/types"
	"go.uber.org/zap"
)

// mockBrainWriter implements scheduler.BrainWriter for testing.
type mockBrainWriter struct {
	dir string
}

func (m *mockBrainWriter) Dir() string { return m.dir }

func (m *mockBrainWriter) Write(ctx context.Context, path, summary, content string) error {
	return nil
}

func TestRegisterSchedulerTools(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	defs, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]bool{
		"cron_create": false,
		"cron_list":   false,
		"cron_delete": false,
		"cron_delay":  false,
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

func TestSchedulerCreate(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	br := &mockBrainWriter{dir: dir}
	sched := scheduler.New(dir, zap.NewNop(), br)
	RegisterSchedulerTools(r, sched)

	args, _ := json.Marshal(map[string]string{
		"name":     "test-task",
		"schedule": "0 * * * *",
		"command":  "echo hello",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-1", Name: "cron_create", Arguments: string(args),
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

	tasks := sched.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "test-task" {
		t.Errorf("expected name 'test-task', got %q", tasks[0].Name)
	}
}

func TestSchedulerCreateInvalidSchedule(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	args, _ := json.Marshal(map[string]string{
		"name":     "bad-task",
		"schedule": "not-a-cron",
		"command":  "echo hello",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-2", Name: "cron_create", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid schedule")
	}
}

func TestSchedulerCreateInvalidArgs(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-3", Name: "cron_create", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSchedulerList(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	br := &mockBrainWriter{dir: dir}
	sched := scheduler.New(dir, zap.NewNop(), br)
	RegisterSchedulerTools(r, sched)

	// Create a task.
	sched.Create(context.Background(), "list-task", "*/5 * * * *", "echo test")

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-4", Name: "cron_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "list-task") {
		t.Errorf("expected 'list-task' in output, got: %s", result.Content)
	}
}

func TestSchedulerListEmpty(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-5", Name: "cron_list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no scheduled tasks") {
		t.Errorf("expected 'no scheduled tasks', got: %s", result.Content)
	}
}

func TestSchedulerDelete(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	br := &mockBrainWriter{dir: dir}
	sched := scheduler.New(dir, zap.NewNop(), br)
	RegisterSchedulerTools(r, sched)

	task, err := sched.Create(context.Background(), "del-task", "0 0 * * *", "echo delete")
	if err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"id": task.ID})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-6", Name: "cron_delete", Arguments: string(args),
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

	if len(sched.List()) != 0 {
		t.Error("expected no tasks after delete")
	}
}

func TestSchedulerDeleteNotFound(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	args, _ := json.Marshal(map[string]string{"id": "nonexistent-id"})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-7", Name: "cron_delete", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for nonexistent task")
	}
}

func TestSchedulerDeleteInvalidArgs(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-8", Name: "cron_delete", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}

func TestSchedulerDelay(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	br := &mockBrainWriter{dir: dir}
	sched := scheduler.New(dir, zap.NewNop(), br)
	RegisterSchedulerTools(r, sched)

	args, _ := json.Marshal(map[string]string{
		"name":    "delayed-task",
		"delay":   "5m",
		"command": "echo delayed",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-9", Name: "cron_delay", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "scheduled") {
		t.Errorf("expected 'scheduled' in response, got: %s", result.Content)
	}

	tasks := sched.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "delayed-task" {
		t.Errorf("expected name 'delayed-task', got %q", tasks[0].Name)
	}
}

func TestSchedulerDelayInvalidDuration(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	args, _ := json.Marshal(map[string]string{
		"name":    "bad-delay",
		"delay":   "not-a-duration",
		"command": "echo bad",
	})
	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-10", Name: "cron_delay", Arguments: string(args),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid delay")
	}
}

func TestSchedulerDelayInvalidArgs(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	sched := scheduler.New(dir, zap.NewNop(), &mockBrainWriter{})
	RegisterSchedulerTools(r, sched)

	result, err := r.Execute(context.Background(), types.ToolCall{
		ID: "call-11", Name: "cron_delay", Arguments: `not json`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invalid args")
	}
}
