package context

import (
	stdctx "context"
	"strings"
)

// Soul reads SOUL.md from workspace or current directory.
type Soul struct {
	Workspace string
}

func (s *Soul) Name() string { return "soul" }
func (s *Soul) Index() int   { return 1 }
func (s *Soul) BuildContent(_ stdctx.Context) (string, error) {
	data, err := searchFiles(s.Workspace, "SOUL.md")
	if err != nil {
		return "", nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}
	return "## Soul\n" + content + "\n", nil
}
