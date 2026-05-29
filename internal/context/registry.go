package context

import (
	stdctx "context"
	"strings"
)

// Section contributes a named section to the system prompt.
type Section interface {
	Name() string
	BuildContent(ctx stdctx.Context) (string, error)
}

// Registry manages registered sections and assembles the final prompt.
type Registry struct {
	sections []Section
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a prompt section. Sections are built in registration order.
func (r *Registry) Register(s Section) {
	r.sections = append(r.sections, s)
}

// Build iterates registered sections and joins non-empty content with "---".
func (r *Registry) Build(ctx stdctx.Context) (string, error) {
	var sb strings.Builder
	for _, section := range r.sections {
		content, err := section.BuildContent(ctx)
		if err != nil {
			return "", err
		}
		if content == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(content)
	}
	return sb.String(), nil
}
