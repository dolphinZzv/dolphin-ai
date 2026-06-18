package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// BrainWriter is the subset of brain.Brain that the scheduler needs.
type BrainWriter interface {
	Dir() string
	Write(ctx context.Context, path, summary, content string) error
}

// Task describes a single scheduled task.
type Task struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Schedule   string     `json:"schedule,omitempty"` // cron expression (recurring)
	Delay      string     `json:"delay,omitempty"`    // duration string like "5m", "1h" (one-shot)
	FireAt     *time.Time `json:"fire_at,omitempty"`  // absolute time for one-shot tasks
	Command    string     `json:"command"`            // shell command
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string     `json:"last_status,omitempty"` // "success" or "failed"
	LastOutput string     `json:"last_output,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	RunCount   int        `json:"run_count"`
}

// runCmd is replaceable in tests.
var runCmd = func(ctx context.Context, command string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	return outb.String(), errb.String(), err
}

// Scheduler manages cron tasks with brain-backed persistence.
type Scheduler struct {
	dir    string
	logger *zap.Logger
	brain  BrainWriter
	cron   *cron.Cron

	mu      sync.Mutex
	tasks   map[string]*Task        // task ID -> task
	entries map[string]cron.EntryID // task ID -> cron entry
	timers  map[string]*time.Timer  // task ID -> timer (one-shot delayed tasks)
}

// New creates a Scheduler. dir is for JSON persistence, brain for markdown output.
func New(dir string, logger *zap.Logger, brain BrainWriter) *Scheduler {
	return &Scheduler{
		dir:     dir,
		logger:  logger,
		brain:   brain,
		cron:    cron.New(),
		tasks:   make(map[string]*Task),
		entries: make(map[string]cron.EntryID),
		timers:  make(map[string]*time.Timer),
	}
}

// Start loads tasks and starts the cron scheduler.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		s.logger.Warn("scheduler: mkdir", zap.Error(err))
	}

	s.loadTasksLocked()
	for _, t := range s.tasks {
		if !t.Enabled {
			continue
		}
		if t.Delay != "" {
			s.scheduleDelayedLocked(t)
		} else if t.Schedule != "" {
			s.addCronLocked(t)
		}
	}
	s.cron.Start()
	s.syncIndexLocked()
	s.logger.Info("scheduler started", zap.Int("tasks", len(s.tasks)))
}

// Stop stops the cron scheduler and all pending timers.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()

	s.mu.Lock()
	for id, tmr := range s.timers {
		tmr.Stop()
		delete(s.timers, id)
	}
	s.mu.Unlock()

	s.logger.Info("scheduler stopped")
}

// Create adds a new scheduled task.
func (s *Scheduler) Create(ctx context.Context, name, schedule, command string) (*Task, error) {
	if _, err := cron.ParseStandard(schedule); err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}
	if name == "" || command == "" {
		return nil, fmt.Errorf("name and command are required")
	}

	t := &Task{
		ID:        uuid.NewString(),
		Name:      name,
		Schedule:  schedule,
		Command:   command,
		Enabled:   true,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[t.ID] = t
	s.addCronLocked(t)
	s.syncTaskToFileLocked(t)
	s.syncBrainLocked(ctx, t)

	s.logger.Info("scheduler: task created", zap.String("name", name), zap.String("id", t.ID))
	return t, nil
}

// List returns all tasks sorted by creation time.
func (s *Scheduler) List() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// ScheduleOnce adds a one-shot delayed task.
func (s *Scheduler) ScheduleOnce(ctx context.Context, name, delay, command string) (*Task, error) {
	d, err := time.ParseDuration(delay)
	if err != nil {
		return nil, fmt.Errorf("invalid delay %q: %w", delay, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("delay must be positive, got %v", d)
	}
	if name == "" || command == "" {
		return nil, fmt.Errorf("name and command are required")
	}

	now := time.Now()
	t := &Task{
		ID:        uuid.NewString(),
		Name:      name,
		Delay:     delay,
		Command:   command,
		FireAt:    timePtr(now.Add(d)),
		Enabled:   true,
		CreatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[t.ID] = t
	s.addTimerLocked(t, d)
	s.syncTaskToFileLocked(t)
	s.syncBrainLocked(ctx, t)

	s.logger.Info("scheduler: delayed task created",
		zap.String("name", name),
		zap.String("id", t.ID[:8]),
		zap.String("fires_at", t.FireAt.Format(time.RFC3339)),
	)
	return t, nil
}

// addTimerLocked creates a time.AfterFunc for a delayed task. Caller must hold s.mu.
func (s *Scheduler) addTimerLocked(t *Task, d time.Duration) {
	if tmr, ok := s.timers[t.ID]; ok {
		tmr.Stop()
	}
	task := t // capture
	s.timers[t.ID] = time.AfterFunc(d, func() {
		s.execute(task)
	})
}

// scheduleDelayedLocked recovers a delayed task on restart. Caller must hold s.mu.
func (s *Scheduler) scheduleDelayedLocked(t *Task) {
	if t.FireAt == nil {
		s.logger.Warn("scheduler: delayed task missing fire_at, skipping",
			zap.String("name", t.Name),
			zap.String("id", t.ID[:8]),
		)
		return
	}
	remaining := time.Until(*t.FireAt)
	if remaining <= 0 {
		s.logger.Info("scheduler: executing overdue delayed task",
			zap.String("name", t.Name),
			zap.Duration("overdue_by", -remaining),
		)
		go s.execute(t)
		return
	}
	s.logger.Info("scheduler: resuming delayed task",
		zap.String("name", t.Name),
		zap.Duration("remaining", remaining),
	)
	s.addTimerLocked(t, remaining)
}

// Upsert creates a new task or updates an existing one by name.
// Returns the task and true if created, false if updated.
func (s *Scheduler) Upsert(ctx context.Context, name, schedule, command string) (*Task, bool, error) {
	if _, err := cron.ParseStandard(schedule); err != nil {
		return nil, false, fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}
	if name == "" || command == "" {
		return nil, false, fmt.Errorf("name and command are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing task with the same name.
	for _, t := range s.tasks {
		if t.Name == name {
			// Remove old cron entry.
			if eid, ok := s.entries[t.ID]; ok {
				s.cron.Remove(eid)
				delete(s.entries, t.ID)
			}
			t.Schedule = schedule
			t.Command = command
			t.Enabled = true
			s.addCronLocked(t)
			s.syncTaskToFileLocked(t)
			s.syncBrainLocked(ctx, t)
			return t, false, nil
		}
	}

	t := &Task{
		ID:        uuid.NewString(),
		Name:      name,
		Schedule:  schedule,
		Command:   command,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	s.tasks[t.ID] = t
	s.addCronLocked(t)
	s.syncTaskToFileLocked(t)
	s.syncBrainLocked(ctx, t)
	return t, true, nil
}

// DeleteByName removes a task by name.
func (s *Scheduler) DeleteByName(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, t := range s.tasks {
		if t.Name == name {
			if eid, ok := s.entries[id]; ok {
				s.cron.Remove(eid)
				delete(s.entries, id)
			}
			if tmr, ok := s.timers[id]; ok {
				tmr.Stop()
				delete(s.timers, id)
			}
			delete(s.tasks, id)
			s.removeTaskFileLocked(id)
			s.deleteBrainLocked(ctx, t)
			return nil
		}
	}
	return fmt.Errorf("task %q not found", name)
}

// Delete removes a task by ID.
func (s *Scheduler) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	if eid, ok := s.entries[id]; ok {
		s.cron.Remove(eid)
		delete(s.entries, id)
	}
	if tmr, ok := s.timers[id]; ok {
		tmr.Stop()
		delete(s.timers, id)
	}
	delete(s.tasks, id)
	s.removeTaskFileLocked(id)
	s.deleteBrainLocked(ctx, t)

	s.logger.Info("scheduler: task deleted", zap.String("name", t.Name), zap.String("id", id))
	return nil
}

// addCronLocked registers a cron entry for the task. Caller must hold s.mu.
func (s *Scheduler) addCronLocked(t *Task) {
	if eid, ok := s.entries[t.ID]; ok {
		s.cron.Remove(eid)
	}
	task := t // capture for closure
	eid, err := s.cron.AddFunc(task.Schedule, func() {
		s.execute(task)
	})
	if err != nil {
		s.logger.Warn("scheduler: add cron failed", zap.String("name", task.Name), zap.Error(err))
		return
	}
	s.entries[task.ID] = eid
}

// execute runs the task's command and updates brain + persistence.
func (s *Scheduler) execute(t *Task) {
	s.logger.Info("scheduler: executing task", zap.String("name", t.Name))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	stdout, stderr, err := runCmd(ctx, t.Command)
	elapsed := time.Since(start)

	s.mu.Lock()
	defer s.mu.Unlock()

	// If the task was deleted while we were running, don't recreate it.
	if _, exists := s.tasks[t.ID]; !exists {
		return
	}

	t.RunCount++
	t.LastRunAt = timePtr(time.Now())

	if err != nil {
		t.LastStatus = "failed"
		t.LastError = truncate(err.Error(), 1024)
		t.LastOutput = truncate(stderr, 4096)
		if t.LastOutput == "" {
			t.LastOutput = truncate(stdout, 4096)
		}
		s.logger.Warn("scheduler: task failed",
			zap.String("name", t.Name),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
	} else {
		t.LastStatus = "success"
		t.LastOutput = truncate(stdout, 4096)
		t.LastError = ""
		s.logger.Info("scheduler: task succeeded",
			zap.String("name", t.Name),
			zap.Duration("elapsed", elapsed),
		)
	}

	s.syncTaskToFileLocked(t)
	s.syncBrainLocked(ctx, t)

	if t.Delay != "" {
		s.deleteTaskLocked(t.ID)
	}
}

// deleteTaskLocked removes a task from maps and persistence. Caller must hold s.mu.
func (s *Scheduler) deleteTaskLocked(id string) {
	if eid, ok := s.entries[id]; ok {
		s.cron.Remove(eid)
		delete(s.entries, id)
	}
	if tmr, ok := s.timers[id]; ok {
		tmr.Stop()
		delete(s.timers, id)
	}
	delete(s.tasks, id)
	s.removeTaskFileLocked(id)
}

// syncBrainLocked writes/updates the task's brain file. Caller must hold s.mu.
func (s *Scheduler) syncBrainLocked(ctx context.Context, t *Task) {
	if s.brain == nil {
		return
	}
	path := filepath.Join("scheduler", safeFilename(t.Name)+".md")
	if err := s.brain.Write(ctx, path, "cron: update "+t.Name, formatBrainFile(t)); err != nil {
		s.logger.Warn("scheduler: brain write failed", zap.String("name", t.Name), zap.Error(err))
	}
}

// deleteBrainLocked marks the task as deleted in the brain file.
func (s *Scheduler) deleteBrainLocked(ctx context.Context, t *Task) {
	if s.brain == nil {
		return
	}
	path := filepath.Join("scheduler", safeFilename(t.Name)+".md")
	verb := "deleted"
	prefix := "cron"
	if t.Delay != "" {
		verb = "completed"
		prefix = "delay"
	}
	content := fmt.Sprintf("# %s\n\n*Task %s at %s*\n", t.Name, verb, time.Now().Format(time.RFC3339))
	if err := s.brain.Write(ctx, path, prefix+": delete "+t.Name, content); err != nil {
		s.logger.Warn("scheduler: brain delete failed", zap.String("name", t.Name), zap.Error(err))
	}
}

// formatBrainFile renders the task as a Markdown file for the brain.
func formatBrainFile(t *Task) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", t.Name)
	if t.Delay != "" {
		b.WriteString("- 一次性延迟: `" + t.Delay + "`\n")
		if t.FireAt != nil {
			b.WriteString("- 执行时间: " + t.FireAt.Format("2006-01-02 15:04:05") + "\n")
		}
	} else {
		b.WriteString("- 定时: `" + t.Schedule + "`\n")
	}
	b.WriteString("- 命令: `" + t.Command + "`\n")
	if t.Enabled {
		b.WriteString("- 状态: 启用\n")
	} else {
		b.WriteString("- 状态: 禁用\n")
	}
	fmt.Fprintf(&b, "- 创建: %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- 执行次数: %d\n", t.RunCount)

	if t.LastRunAt != nil {
		fmt.Fprintf(&b, "\n## 最近执行 (%s)\n\n", t.LastRunAt.Format("2006-01-02 15:04:05"))
		b.WriteString("- 状态: " + t.LastStatus + "\n")
		if t.LastOutput != "" {
			b.WriteString("- 输出:\n```\n" + t.LastOutput + "\n```\n")
		}
		if t.LastError != "" {
			b.WriteString("- 错误:\n```\n" + t.LastError + "\n```\n")
		}
	} else if t.Delay != "" && t.FireAt != nil {
		fmt.Fprintf(&b, "\n## 待执行\n\n- 等待执行: %s\n", t.FireAt.Format("2006-01-02 15:04:05"))
	}

	return b.String()
}

// loadTasksLocked reads all task markdown files from the scheduler directory. Caller must hold s.mu.
func (s *Scheduler) loadTasksLocked() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "index.md" || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var t Task
		if err := yaml.Unmarshal(data, &t); err != nil {
			s.logger.Warn("scheduler: corrupted task file", zap.String("file", entry.Name()), zap.Error(err))
			continue
		}
		s.tasks[t.ID] = &t
	}
}

// syncTaskToFileLocked writes a single task to its file. Caller must hold s.mu.
func (s *Scheduler) syncTaskToFileLocked(t *Task) {
	if s.dir == "" {
		return
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		s.logger.Warn("scheduler: marshal task", zap.String("name", t.Name), zap.Error(err))
		return
	}
	content := "---\n" + string(data) + "---\n"
	if err := os.WriteFile(filepath.Join(s.dir, t.ID+".md"), []byte(content), 0o600); err != nil {
		s.logger.Warn("scheduler: save task file", zap.String("name", t.Name), zap.Error(err))
	}
	s.syncIndexLocked()
}

// removeTaskFileLocked deletes a task's file. Caller must hold s.mu.
func (s *Scheduler) removeTaskFileLocked(id string) {
	if s.dir == "" {
		return
	}
	if err := os.Remove(filepath.Join(s.dir, id+".md")); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("scheduler: remove task file", zap.String("id", id), zap.Error(err))
	}
	s.syncIndexLocked()
}

// syncIndexLocked writes index.md listing all tasks. Caller must hold s.mu.
func (s *Scheduler) syncIndexLocked() {
	if s.dir == "" {
		return
	}
	var b strings.Builder
	b.WriteString("# Scheduled Tasks\n\n")
	if len(s.tasks) == 0 {
		b.WriteString("No tasks registered.\n")
	} else {
		b.WriteString("| ID | Name | Type | Schedule | Command | Status | Runs |\n")
		b.WriteString("|---|---|---|---|---|---|---|\n")
		for _, t := range s.tasks {
			sched := t.Schedule
			typ := "cron"
			if t.Delay != "" {
				sched = t.Delay
				typ = "delay"
			}
			status := "pending"
			if t.LastStatus != "" {
				status = t.LastStatus
			}
			if !t.Enabled {
				status = "disabled"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %d |\n",
				t.ID[:8], t.Name, typ, sched, t.Command, status, t.RunCount)
		}
	}
	if err := os.WriteFile(filepath.Join(s.dir, "index.md"), []byte(b.String()), 0o600); err != nil {
		s.logger.Warn("scheduler: write index.md", zap.Error(err))
	}
}

func safeFilename(name string) string {
	r := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", " ", "-",
		"|", "-", "*", "", "?", "", "<", "", ">", "", `"`, "",
	)
	return r.Replace(name)
}

func timePtr(t time.Time) *time.Time { return &t }

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
