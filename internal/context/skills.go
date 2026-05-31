package context

import (
	stdctx "context"
	"strings"

	"dolphin/internal/skill"
)

// Skills injects descriptions of enabled skills.
type Skills struct {
	Store skill.Store
}

func (s *Skills) Name() string { return "skills" }
func (s *Skills) Index() int   { return 7 }
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
