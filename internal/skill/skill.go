// Package skill manages agent skill definitions and execution.
package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Skill represents a named, specialized capability with descriptive content.
type Skill struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Source      string    `json:"source"` // directory origin, e.g. "~/.dolphin/skills"
	CallCount   int64     `json:"call_count"`
	LastCalled  time.Time `json:"last_called_at"`
}

// Manager loads and manages skills from directories of markdown files.
// Multiple directories are supported: later directories override earlier ones on name conflict.
type Manager struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	dirs   []string
}

// NewManager creates a skill manager from one or more directories.
// Skills are loaded from all directories; later directories override earlier ones.
// Empty strings are filtered out. If no dirs remain, defaults to [".dolphin/skills"].
func NewManager(dirs ...string) *Manager {
	filtered := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{".dolphin/skills"}
	}
	return &Manager{
		skills: make(map[string]*Skill),
		dirs:   filtered,
	}
}

// Dir returns the primary skills directory path.
func (m *Manager) Dir() string {
	if len(m.dirs) > 0 {
		return m.dirs[0]
	}
	return ""
}

// Dirs returns all configured skills directories.
func (m *Manager) Dirs() []string { return m.dirs }

// Load scans all skills directories and loads all skill files.
// Missing or unreadable directories are silently skipped.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear and reload from all dirs
	m.skills = make(map[string]*Skill)

	for _, dir := range m.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read skills dir %q: %w", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				// New structure: <name>/SKILL.md
				skillPath := filepath.Join(dir, name, "SKILL.md")
				data, err := os.ReadFile(skillPath)
				if err != nil {
					continue
				}
				skill := parseSkillFile(data, name+".md")
				if skill != nil {
					skill.Source = dir
					m.skills[skill.Name] = skill
				}
			} else if strings.HasSuffix(name, ".md") {
				// Backward compat: flat .md files at top level
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					continue
				}
				skill := parseSkillFile(data, name)
				if skill != nil {
					skill.Source = dir
					m.skills[skill.Name] = skill
				}
			}
		}
	}
	return nil
}

// Get returns a skill by name.
func (m *Manager) Get(name string) (*Skill, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// List returns all skills sorted by name.
func (m *Manager) List() []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Skill, 0, len(m.skills))
	for _, s := range m.skills {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// TopSkills returns the top n most-used skills by call count.
func (m *Manager) TopSkills(n int) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	type entry struct {
		skill *Skill
		cnt   int64
	}
	var list []entry
	for _, s := range m.skills {
		list = append(list, entry{s, s.CallCount})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].cnt != list[j].cnt {
			return list[i].cnt > list[j].cnt
		}
		return list[i].skill.Name < list[j].skill.Name
	})

	if n > len(list) {
		n = len(list)
	}
	skills := make([]*Skill, n)
	for i := 0; i < n; i++ {
		skills[i] = list[i].skill
	}
	return skills
}

// ListForAgent returns skills filtered by the allowed list.
// If allowed is empty, returns all skills (backward compatible).
func (m *Manager) ListForAgent(allowed []string) []*Skill {
	all := m.List()
	if len(allowed) == 0 {
		return all
	}
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	filtered := make([]*Skill, 0, len(allowed))
	for _, s := range all {
		if set[s.Name] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// GetForAgent returns a skill by name if it is in the allowed list.
// If allowed is empty, returns the skill (backward compatible).
func (m *Manager) GetForAgent(name string, allowed []string) (*Skill, bool) {
	if len(allowed) > 0 {
		ok := false
		for _, a := range allowed {
			if a == name {
				ok = true
				break
			}
		}
		if !ok {
			return nil, false
		}
	}
	return m.Get(name)
}

// Search returns skills whose name or description matches the query.
func (m *Manager) Search(query string) []*Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	q := strings.ToLower(query)
	var results []*Skill
	for _, s := range m.skills {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) {
			results = append(results, s)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// RecordUsage increments the call count for a skill and updates LastCalled.
func (m *Manager) RecordUsage(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.skills[name]; ok {
		s.CallCount++
		s.LastCalled = time.Now()
	}
}

// NewTemplate creates a new skill file from a template in the primary skills directory.
func (m *Manager) NewTemplate(name, description string) error {
	if description == "" {
		description = name
	}
	title := name
	if description != name {
		title = description
	}
	content := fmt.Sprintf("# %s\n\n"+
		"Add your skill content here. This content is injected into the LLM context\n"+
		"when the skill is loaded with load_skill.\n\n"+
		"## Overview\n\n"+
		"Briefly describe what this skill covers and when to use it.\n\n"+
		"## Guidelines\n\n"+
		"- Keep instructions clear and actionable.\n"+
		"- Provide concrete examples where possible.\n"+
		"- Focus on what the agent needs to know to perform the task.\n\n"+
		"## Examples\n\n"+
		"Add usage examples or code snippets here.\n", title)
	return m.Register(name, description, content)
}

// Register adds or updates a skill at runtime and persists it to the primary
// skills directory as a markdown file.
func (m *Manager) Register(name, description, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill := &Skill{
		Name:        name,
		Description: description,
		Content:     content,
		Source:      m.dirs[0],
		CallCount:   0,
	}

	m.skills[name] = skill

	// Persist to disk
	dir := m.dirs[0]
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}

	var sb strings.Builder
	if description != "" {
		fmt.Fprintf(&sb, "---\nname: %s\ndescription: %s\n---\n\n", name, description)
	}
	sb.WriteString(content)

	return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(sb.String()), 0600)
}

// Unregister removes a skill from memory and deletes its directory from where
// it was loaded (user home or project dir).
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(m.skills, name)

	skillDir := filepath.Join(s.Source, name)
	if err := os.RemoveAll(skillDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Disable removes a skill from memory and renames its directory to <name>.disabled/,
// preserving files on disk so it can be re-enabled later.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(m.skills, name)

	oldDir := filepath.Join(s.Source, name)
	newDir := filepath.Join(s.Source, name+".disabled")
	if err := os.Rename(oldDir, newDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("disable skill %q: %w", name, err)
	}
	return nil
}

// Enable restores a previously disabled skill by renaming <name>.disabled/ back
// to <name>/ and reloading it into memory. Searches all configured directories
// for the .disabled directory.
func (m *Manager) Enable(name string) error {
	var disabledDir, skillDir string
	found := false
	for _, dir := range m.dirs {
		dd := filepath.Join(dir, name+".disabled")
		if _, err := os.Stat(dd); err == nil {
			disabledDir = dd
			skillDir = filepath.Join(dir, name)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("disabled skill %q not found", name)
	}

	if err := os.Rename(disabledDir, skillDir); err != nil {
		return fmt.Errorf("enable skill %q: %w", name, err)
	}

	// Reload to pick up the re-enabled skill
	return m.Load()
}

// Reload re-scans all skill directories. This is the hot-reload entry point.
func (m *Manager) Reload() error { return m.Load() }

// WatchAndReload periodically reloads skills when directory mtimes change.
// This follows the ticker-based polling pattern used elsewhere in the codebase.
func (m *Manager) WatchAndReload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastMod time.Time
	for _, dir := range m.dirs {
		if info, err := os.Stat(dir); err == nil {
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var latest time.Time
			for _, dir := range m.dirs {
				if info, err := os.Stat(dir); err == nil {
					if info.ModTime().After(latest) {
						latest = info.ModTime()
					}
				}
			}
			if latest.After(lastMod) {
				if err := m.Reload(); err != nil {
					fmt.Fprintf(os.Stderr, "[skills] reload error: %v\n", err)
				}
				lastMod = latest
			}
		}
	}
}

// parseSkillFile parses a markdown file with optional YAML frontmatter.
// Expected format:
//
//	---
//	name: skill-name
//	description: ...
//	---
//	<markdown content>
func parseSkillFile(data []byte, filename string) *Skill {
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	skill := &Skill{
		Name:    strings.TrimSuffix(filename, ".md"),
		Content: content,
	}

	// Check for frontmatter (bounded by ---)
	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx > 0 {
			frontmatter := rest[:endIdx]
			body := strings.TrimSpace(rest[endIdx+4:])

			// Parse YAML frontmatter
			var fm struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			dec := yaml.NewDecoder(strings.NewReader(frontmatter))
			dec.KnownFields(true)
			if err := dec.Decode(&fm); err == nil {
				if fm.Name != "" {
					skill.Name = fm.Name
				}
				skill.Description = fm.Description
			}

			if body != "" {
				skill.Content = body
			}
		}
	}

	return skill
}
