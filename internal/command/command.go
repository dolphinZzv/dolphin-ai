// Package command provides user-defined /commands that are triggered by user input.
// Unlike skills (LLM-invoked), commands are explicitly invoked by typing /<name>.
package command

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Command represents a user-defined /command with instructions for the LLM.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"` // markdown body sent to LLM when triggered
	Source      string `json:"source"`  // directory origin
	CallCount   int64  `json:"call_count"`
}

// Manager loads and manages commands from directories of markdown files.
// Multiple directories are supported: later directories override earlier ones on name conflict.
type Manager struct {
	mu   sync.RWMutex
	cmds map[string]*Command
	dirs []string
}

// NewManager creates a command manager from one or more directories.
// Empty strings are filtered out. If no dirs remain, defaults to [".dolphinzZ/commands"].
func NewManager(dirs ...string) *Manager {
	filtered := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{".dolphinzZ/commands"}
	}
	return &Manager{
		cmds: make(map[string]*Command),
		dirs: filtered,
	}
}

// Dirs returns all configured commands directories.
func (m *Manager) Dirs() []string { return m.dirs }

// Load scans all commands directories and loads all command files.
// Missing or unreadable directories are silently skipped.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cmds = make(map[string]*Command)

	for _, dir := range m.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read commands dir %q: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			cmd := parseCommandFile(data, entry.Name())
			if cmd != nil {
				cmd.Source = dir
				m.cmds[cmd.Name] = cmd
			}
		}
	}
	return nil
}

// Get returns a command by name.
func (m *Manager) Get(name string) (*Command, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.cmds[name]
	return c, ok
}

// List returns all commands sorted by name.
func (m *Manager) List() []*Command {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Command, 0, len(m.cmds))
	for _, c := range m.cmds {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// RecordUsage increments the call count for a command.
func (m *Manager) RecordUsage(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.cmds[name]; ok {
		c.CallCount++
	}
}

// parseCommandFile parses a markdown file with optional YAML frontmatter.
// Expected format:
//
//	---
//	name: command-name
//	description: ...
//	---
//	<markdown content>
func parseCommandFile(data []byte, filename string) *Command {
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	cmd := &Command{
		Name:    strings.TrimSuffix(filename, ".md"),
		Content: content,
	}

	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx > 0 {
			frontmatter := rest[:endIdx]
			body := strings.TrimSpace(rest[endIdx+4:])

			var fm struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(frontmatter), &fm); err == nil {
				if fm.Name != "" {
					cmd.Name = fm.Name
				}
				cmd.Description = fm.Description
			}

			cmd.Content = body
		}
	}

	return cmd
}
