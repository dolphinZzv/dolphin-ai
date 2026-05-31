package context

import (
	stdctx "context"
	"strings"
)

// Design reads DESIGN.md from workspace or current directory.
type Design struct {
	Workspace string
}

func (s *Design) Name() string { return "design" }
func (s *Design) Index() int   { return 6 }
func (s *Design) BuildContent(_ stdctx.Context) (string, error) {
	data, err := searchFiles(s.Workspace, "DESIGN.md")
	if err != nil {
		return "", nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}
	return "## Design Notes\n" + content + "\n", nil
}
