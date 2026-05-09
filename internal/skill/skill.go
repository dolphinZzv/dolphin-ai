package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Skill represents a named, specialized capability with descriptive content.
type Skill struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	CallCount   int64     `json:"call_count"`
	LastCalled  time.Time `json:"last_called_at"`
}

// Manager loads and manages skills from a directory of markdown files.
type Manager struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	dir    string
}

// NewManager creates a skill manager. If dir is empty, uses ".dolphinzZ/skills".
func NewManager(dir string) *Manager {
	if dir == "" {
		dir = ".dolphinzZ/skills"
	}
	return &Manager{
		skills: make(map[string]*Skill),
		dir:    dir,
	}
}

// Dir returns the skills directory path.
func (m *Manager) Dir() string { return m.dir }

// Load scans the skills directory and loads all skill files.
// Returns an error if the directory doesn't exist (caller should check).
func (m *Manager) Load() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read skills dir %q: %w", m.dir, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		skill := parseSkillFile(data, entry.Name())
		if skill != nil {
			m.skills[skill.Name] = skill
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

			// Parse simple YAML key: value lines
			for _, line := range strings.Split(frontmatter, "\n") {
				line = strings.TrimSpace(line)
				if idx := strings.Index(line, ":"); idx > 0 {
					key := strings.TrimSpace(line[:idx])
					val := strings.TrimSpace(line[idx+1:])
					val = strings.Trim(val, `"'`)
					switch key {
					case "name":
						if val != "" {
							skill.Name = val
						}
					case "description":
						skill.Description = val
					}
				}
			}

			if body != "" {
				skill.Content = body
			}
		}
	}

	return skill
}
