package context

import (
	stdctx "context"
	"os"
	"path/filepath"
)

// searchFiles looks for the first readable file from the given paths.
// Workspace path is checked first if non-empty, then bare filename.
func searchFiles(workspace string, names ...string) ([]byte, error) {
	for _, name := range names {
		if workspace != "" {
			data, err := os.ReadFile(filepath.Join(workspace, name))
			if err == nil {
				return data, nil
			}
		}
		data, err := os.ReadFile(name)
		if err == nil {
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}

// Base section reads AGENTS.md or CLAUDE.md, or falls back to default text.
type Base struct {
	Workspace   string
	DefaultText string
}

func (s *Base) Name() string { return "base" }
func (s *Base) Index() int   { return 0 }
func (s *Base) BuildContent(_ stdctx.Context) (string, error) {
	if s.DefaultText != "" {
		return s.DefaultText, nil
	}
	data, err := searchFiles(s.Workspace, "AGENTS.md", "CLAUDE.md")
	if err == nil {
		return string(data), nil
	}
	return "You are Dolphin, an AI assistant.", nil
}
