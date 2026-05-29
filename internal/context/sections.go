package context

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/skill"
)

// BrainIndexReader provides the brain index content to inject into system prompt.
type BrainIndexReader interface {
	ReadIndex(ctx stdctx.Context) (string, error)
}

// Base section reads AGENTS.md or CLAUDE.md, or falls back to default text.
type Base struct {
	Workspace   string
	DefaultText string
}

func (s *Base) Name() string { return "base" }

func (s *Base) BuildContent(_ stdctx.Context) (string, error) {
	if s.DefaultText != "" {
		return s.DefaultText, nil
	}
	if s.Workspace != "" {
		if data, err := os.ReadFile(filepath.Join(s.Workspace, "AGENTS.md")); err == nil {
			return string(data), nil
		} else if data, err := os.ReadFile(filepath.Join(s.Workspace, "CLAUDE.md")); err == nil {
			return string(data), nil
		}
	}
	return "You are Dolphin, an AI assistant.", nil
}

// Transport injects transport-specific context.
type Transport struct {
	ContextFunc func() string
}

func (s *Transport) Name() string { return "transport" }

func (s *Transport) BuildContent(_ stdctx.Context) (string, error) {
	ctx := s.ContextFunc()
	if ctx == "" {
		return "", nil
	}
	return "## Transport Context\n" + ctx + "\n", nil
}

// Workspace injects workspace directory info.
type Workspace struct {
	Dir string
}

func (s *Workspace) Name() string { return "workspace" }

func (s *Workspace) BuildContent(_ stdctx.Context) (string, error) {
	if s.Dir == "" {
		return "", nil
	}
	return "## Workspace\nYour workspace directory is `" + s.Dir + "`. Use the `exec` tool to run commands there.\n", nil
}

// Brain injects brain index.
type Brain struct {
	Reader BrainIndexReader
}

func (s *Brain) Name() string { return "brain" }

func (s *Brain) BuildContent(ctx stdctx.Context) (string, error) {
	if s.Reader == nil {
		return "", nil
	}
	idx, err := s.Reader.ReadIndex(ctx)
	if err != nil || idx == "" {
		return "", nil
	}
	return "## Brain Index\nThe following is an index of my long-term knowledge directory. Use brain_read / brain_write tools to access specific files.\n\n" + idx, nil
}

// Design reads DESIGN.md from workspace.
type Design struct {
	Workspace string
}

func (s *Design) Name() string { return "design" }

func (s *Design) BuildContent(_ stdctx.Context) (string, error) {
	if s.Workspace == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(s.Workspace, "DESIGN.md"))
	if err != nil {
		return "", nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}
	return "## Design Notes\n" + content + "\n", nil
}

// Skills injects descriptions of enabled skills.
type Skills struct {
	Store skill.Store
}

func (s *Skills) Name() string { return "skills" }

func (s *Skills) BuildContent(ctx stdctx.Context) (string, error) {
	if s.Store == nil {
		return "", nil
	}
	skills, err := s.Store.List(ctx)
	if err != nil {
		return "", nil
	}
	var sb strings.Builder
	for i, sk := range skills {
		if !sk.Enabled || sk.Name == "" {
			continue
		}
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString("## Skill: ")
		sb.WriteString(sk.Name)
		sb.WriteString("\n")
		sb.WriteString(sk.Description)
	}
	return sb.String(), nil
}
