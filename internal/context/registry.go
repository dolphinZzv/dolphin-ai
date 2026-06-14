package context

import (
	stdctx "context"
	"sort"
	"strings"
)

// Section contributes a named section to the system prompt.
type Section interface {
	Name() string
	Index() int
	BuildContent(ctx stdctx.Context) (string, error)
}

// Registry manages registered sections and assembles the final prompt.
type Registry struct {
	sections []Section
}

func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a prompt section.
func (r *Registry) Register(s Section) {
	r.sections = append(r.sections, s)
}

// Sections returns registered sections sorted by Index.
func (r *Registry) Sections() []Section {
	ordered := make([]Section, len(r.sections))
	copy(ordered, r.sections)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Index() < ordered[j].Index()
	})
	return ordered
}

// ByName returns the section with the given name, or false if not found.
func (r *Registry) ByName(name string) (Section, bool) {
	for _, s := range r.sections {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// Build iterates registered sections sorted by Index and joins non-empty content.
func (r *Registry) Build(ctx stdctx.Context) (string, error) {
	var sb strings.Builder
	for _, section := range r.Sections() {
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
