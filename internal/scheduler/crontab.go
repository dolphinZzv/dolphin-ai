// Package scheduler manages cron-like scheduled tasks defined in CRONTAB.md.
package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// CronTask is a single scheduled task from CRONTAB.md.
type CronTask struct {
	Name        string `yaml:"name"`
	Schedule    string `yaml:"schedule"` // 5-field cron expression
	Description string `yaml:"description"`
	Enabled     bool   `yaml:"enabled"`
	Task        string `yaml:"-"` // markdown body — what the agent should do
	lastRun     time.Time
}

// CronResult stores the outcome of a single task execution.
type CronResult struct {
	TaskName    string    `json:"task_name"`
	Success     bool      `json:"success"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

// Manager manages CRONTAB.md loading, scheduling, and task lifecycle.
type Manager struct {
	cfg           config.CrontabConfig
	filePath      string
	mu            sync.RWMutex
	tasks         []*CronTask
	dueCh         chan CronTask
	results       []CronResult
	resultsMu     sync.Mutex
	parser        cron.Parser
	checkInterval time.Duration
	cancel        context.CancelFunc
}

// NewManager creates a new cron task manager.
func NewManager(cfg config.CrontabConfig) *Manager {
	interval, _ := time.ParseDuration(cfg.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Manager{
		cfg:           cfg,
		filePath:      cfg.File,
		dueCh:         make(chan CronTask, 100),
		parser:        cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		checkInterval: interval,
	}
}

// Load reads and parses CRONTAB.md. If the file is missing, corrupted, or entries
// have invalid format, the manager recovers gracefully and will not block startup.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist — create a fresh one with a header
			//nolint:govet
			if err := m.writeEmptyFile(); err != nil {
				return fmt.Errorf("create crontab file: %w", err)
			}
			zap.S().Infow("crontab file created", "file", m.filePath)
			m.tasks = nil
			return nil
		}
		return fmt.Errorf("read crontab file: %w", err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		m.tasks = nil
		return nil
	}

	tasks, err := parseEntries(data)
	if err != nil {
		zap.S().Warnw("parsing crontab entries", "error", err)
	}
	if tasks == nil {
		m.tasks = nil
	} else {
		m.tasks = tasks
	}
	zap.S().Infow("crontab loaded", "file", m.filePath, "tasks", len(m.tasks))
	return nil
}

// writeEmptyFile creates a new CRONTAB.md with a header comment.
func (m *Manager) writeEmptyFile() error {
	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	header := `# CRONTAB.md — Scheduled tasks
# Each entry: YAML frontmatter between --- delimiters, followed by task instructions.
# Fields: name, schedule (5-field cron), description, enabled (true/false).

`
	return os.WriteFile(m.filePath, []byte(header), 0600)
}

// parseEntries parses all entries from CRONTAB.md content line by line.
func parseEntries(data []byte) ([]*CronTask, error) {
	lines := strings.Split(string(data), "\n")
	var tasks []*CronTask
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	i := 0
	// Skip header lines until first ---
	for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
		i++
	}

	for i < len(lines) {
		// Skip entry separators (consecutive --- lines between entries)
		if strings.TrimSpace(lines[i]) == "---" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "---" {
			i++
			continue
		}

		// Expect opening ---
		if strings.TrimSpace(lines[i]) != "---" {
			i++
			continue
		}
		i++ // skip ---

		// Collect YAML frontmatter lines
		var yamlLines []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
			yamlLines = append(yamlLines, lines[i])
			i++
		}
		if i >= len(lines) {
			break // unclosed frontmatter, stop
		}
		i++ // skip closing ---

		// Collect body lines until next standalone ---
		var bodyLines []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
			bodyLines = append(bodyLines, lines[i])
			i++
		}
		// i now points at --- or end of file

		// Parse frontmatter YAML
		var task CronTask
		dec := yaml.NewDecoder(strings.NewReader(strings.Join(yamlLines, "\n")))
		dec.KnownFields(true)
		if err := dec.Decode(&task); err != nil {
			zap.S().Warnw("skipping crontab entry with invalid frontmatter", "error", err)
			continue
		}
		if task.Name == "" {
			zap.S().Warnw("skipping crontab entry without name")
			continue
		}
		if task.Schedule == "" {
			zap.S().Warnw("skipping crontab entry without schedule", "name", task.Name)
			continue
		}

		// Validate cron expression
		if _, err := parser.Parse(task.Schedule); err != nil {
			task.Enabled = false
			zap.S().Warnw("invalid cron expression, disabling task", "name", task.Name, "schedule", task.Schedule, "error", err)
		}

		task.Task = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		tasks = append(tasks, &task)
	}

	if len(tasks) == 0 && len(lines) > 0 {
		// Only return error if we found --- markers but no valid entries
		hasMarkers := false
		for _, l := range lines {
			if strings.TrimSpace(l) == "---" {
				hasMarkers = true
				break
			}
		}
		if hasMarkers {
			// Empty — not an error
			return nil, nil
		}
	}

	return tasks, nil
}

// OnConfigChange handles crontab config hot-reload. Updates the internal config
// and reloads the crontab file.
func (m *Manager) OnConfigChange(oldCfg, newCfg *config.Config) {
	m.mu.Lock()
	m.cfg = newCfg.Crontab
	m.filePath = newCfg.Crontab.File
	interval, _ := time.ParseDuration(newCfg.Crontab.CheckInterval)
	if interval <= 0 {
		interval = 30 * time.Second
	}
	m.checkInterval = interval
	m.mu.Unlock()

	// Reload tasks from the (possibly new) crontab file.
	// Note: Load() acquires its own lock, so we must release ours first.
	if err := m.Load(); err != nil {
		zap.S().Warnw("crontab reload failed after config change", "error", err)
	}
}

// Start launches the background ticker that checks for due tasks.
// Returns the due tasks channel for the consumer (coordinator) to drain.
func (m *Manager) Start(ctx context.Context) <-chan CronTask {
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	go m.runLoop(ctx)
	return m.dueCh
}

// Stop stops the background ticker.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// runLoop is the background ticker goroutine.
func (m *Manager) runLoop(ctx context.Context) {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// Check once immediately on start
	m.checkDue()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkDue()
		}
	}
}

// checkDue checks all tasks and pushes due ones to the channel.
func (m *Manager) checkDue() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, task := range m.tasks {
		if !task.Enabled {
			continue
		}
		sched, err := m.parser.Parse(task.Schedule)
		if err != nil {
			continue
		}
		next := sched.Next(task.lastRun)
		if next.After(now) {
			continue
		}
		task.lastRun = now

		// Non-blocking push — drop if channel full (shouldn't happen with buffer 100)
		select {
		case m.dueCh <- *task:
		default:
			zap.S().Warnw("cron due channel full, dropping task", "name", task.Name)
		}
	}
}

// AddTask appends a task to CRONTAB.md and adds it to the in-memory list.
func (m *Manager) AddTask(task *CronTask) error {
	// Validate cron expression
	if _, err := m.parser.Parse(task.Schedule); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", task.Schedule, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check name uniqueness
	for _, t := range m.tasks {
		if t.Name == task.Name {
			return fmt.Errorf("task %q already exists", task.Name)
		}
	}

	// Serialize entry
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any{
		"name":        task.Name,
		"schedule":    task.Schedule,
		"description": task.Description,
		"enabled":     task.Enabled,
	}); err != nil {
		return fmt.Errorf("encode frontmatter: %w", err)
	}
	enc.Close()
	buf.WriteString("---\n")
	if task.Task != "" {
		buf.WriteString(task.Task)
		buf.WriteString("\n")
	}
	buf.WriteString("\n")

	// Append to file
	f, err := os.OpenFile(m.filePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("open crontab file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write crontab entry: %w", err)
	}

	// Add to memory
	task.lastRun = time.Time{}
	m.tasks = append(m.tasks, task)
	return nil
}

// RemoveTask removes a task by name from both file and memory.
func (m *Manager) RemoveTask(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find index
	idx := -1
	for i, t := range m.tasks {
		if t.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}

	// Remove from memory
	m.tasks = append(m.tasks[:idx], m.tasks[idx+1:]...)

	// Rewrite file
	m.rewriteFileLocked()
	return true
}

// ToggleTask enables or disables a task.
func (m *Manager) ToggleTask(name string, enabled bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tasks {
		if t.Name == name {
			t.Enabled = enabled
			m.rewriteFileLocked()
			return true
		}
	}
	return false
}

// List returns a snapshot of all tasks.
func (m *Manager) List() []CronTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]CronTask, len(m.tasks))
	for i, t := range m.tasks {
		list[i] = *t
	}
	return list
}

// Get returns a task by name.
func (m *Manager) Get(name string) (CronTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.tasks {
		if t.Name == name {
			return *t, true
		}
	}
	return CronTask{}, false
}

// AddResult stores a task execution result (max 100 results kept).
func (m *Manager) AddResult(taskName string, success bool, output, errMsg string) {
	m.resultsMu.Lock()
	defer m.resultsMu.Unlock()

	r := CronResult{
		TaskName:    taskName,
		Success:     success,
		Output:      output,
		Error:       errMsg,
		CompletedAt: time.Now(),
	}
	m.results = append(m.results, r)
	if len(m.results) > 100 {
		m.results = m.results[len(m.results)-100:]
	}
}

// PendingResults returns all results since the last call and clears them.
func (m *Manager) PendingResults() []CronResult {
	m.resultsMu.Lock()
	defer m.resultsMu.Unlock()

	if len(m.results) == 0 {
		return nil
	}
	r := make([]CronResult, len(m.results))
	copy(r, m.results)
	m.results = m.results[:0]
	return r
}

// rewriteFileLocked rewrites the entire CRONTAB.md from in-memory tasks.
// Must be called with m.mu held (write lock).
func (m *Manager) rewriteFileLocked() {
	var buf bytes.Buffer
	buf.WriteString("# CRONTAB.md — Scheduled tasks\n")
	buf.WriteString("# Each entry: YAML frontmatter between --- delimiters, followed by task instructions.\n")
	buf.WriteString("# Fields: name, schedule (5-field cron), description, enabled (true/false).\n\n")

	for _, task := range m.tasks {
		buf.WriteString("---\n")
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		enc.Encode(map[string]any{
			"name":        task.Name,
			"schedule":    task.Schedule,
			"description": task.Description,
			"enabled":     task.Enabled,
		})
		enc.Close()
		buf.WriteString("---\n")
		if task.Task != "" {
			buf.WriteString(task.Task)
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}

	if err := os.WriteFile(m.filePath, buf.Bytes(), 0600); err != nil {
		zap.S().Errorw("write crontab file", "error", err)
	}
}
