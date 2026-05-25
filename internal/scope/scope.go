// Package scope provides directory-level agent routing through scoped
// agent definitions. Scopes map file paths to specialized agents.
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Scope maps a set of directories to a specialized agent.
type Scope struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Dirs        []string `yaml:"dirs"`
	Role        string   `yaml:"role"`
	Tools       []string `yaml:"tools"`
	Skills      []string `yaml:"skills,omitempty"`
	Workflows   []string `yaml:"workflows,omitempty"`
	Context     string   `yaml:"context,omitempty"`
	Timeout     int      `yaml:"timeout,omitempty"`
}

// scopesFile is the top-level YAML structure for scopes.yaml.
type scopesFile struct {
	Scopes []Scope `yaml:"scopes"`
}

// Manager holds all loaded scope definitions and provides lookup and
// file-matching operations.
type Manager struct {
	scopes   []Scope
	byName   map[string]*Scope
	Warnings []string // validation warnings from LoadScopes
}

// LoadScopes reads and validates a scopes.yaml file.
//
// Returns nil, nil if the file does not exist. Returns an error if the
// file exists but cannot be parsed or has no valid scopes.
func LoadScopes(path string) (*Manager, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scopes file: %w", err)
	}

	var sf scopesFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse scopes file: %w", err)
	}

	if len(sf.Scopes) == 0 {
		return nil, nil
	}

	m := &Manager{
		byName: make(map[string]*Scope, len(sf.Scopes)),
	}

	var errs []string
	for i := range sf.Scopes {
		s := &sf.Scopes[i]
		if s.Name == "" {
			errs = append(errs, fmt.Sprintf("scope[%d]: name is required", i))
			continue
		}
		if len(s.Dirs) == 0 {
			errs = append(errs, fmt.Sprintf("scope %q: dirs is required", s.Name))
			continue
		}
		if s.Role == "" {
			errs = append(errs, fmt.Sprintf("scope %q: role is required", s.Name))
			continue
		}
		if _, exists := m.byName[s.Name]; exists {
			errs = append(errs, fmt.Sprintf("scope %q: duplicate name", s.Name))
			continue
		}
		m.scopes = append(m.scopes, *s)
		m.byName[s.Name] = &m.scopes[len(m.scopes)-1]
	}

	if len(errs) > 0 {
		m.Warnings = errs
	}
	if len(m.scopes) == 0 {
		return nil, fmt.Errorf("no valid scopes: %s", strings.Join(errs, "; "))
	}

	return m, nil
}

// Resolve maps file paths to scope names using directory prefix matching.
// A file may match multiple scopes if its path falls under multiple scope
// directories.
func (m *Manager) Resolve(paths []string) map[string][]string {
	result := make(map[string][]string)
	for _, path := range paths {
		for _, s := range m.scopes {
			if matchFile(path, s.Dirs) {
				result[s.Name] = append(result[s.Name], path)
			}
		}
	}
	return result
}

// Scope returns a scope by name, or nil.
func (m *Manager) Scope(name string) *Scope {
	return m.byName[name]
}

// Scopes returns all loaded scopes.
func (m *Manager) Scopes() []Scope {
	out := make([]Scope, len(m.scopes))
	copy(out, m.scopes)
	return out
}

// Info returns lightweight scope info for display.
func (m *Manager) Info() []ScopeInfo {
	infos := make([]ScopeInfo, 0, len(m.scopes))
	for _, s := range m.scopes {
		infos = append(infos, ScopeInfo{
			Name:        s.Name,
			Description: s.Description,
			Dirs:        s.Dirs,
		})
	}
	return infos
}

// matchFile checks whether a file path falls under any of the given directories.
func matchFile(file string, dirs []string) bool {
	for _, dir := range dirs {
		normDir := filepath.ToSlash(dir)
		normFile := filepath.ToSlash(file)
		if strings.HasPrefix(normFile, normDir+"/") || normFile == normDir {
			return true
		}
	}
	return false
}
