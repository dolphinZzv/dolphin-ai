package context

import stdctx "context"

// Workspace injects workspace directory info.
type Workspace struct {
	Dir string
}

func (s *Workspace) Name() string { return "workspace" }
func (s *Workspace) Index() int   { return 4 }
func (s *Workspace) BuildContent(_ stdctx.Context) (string, error) {
	if s.Dir == "" {
		return "", nil
	}
	return "## Workspace\nYour workspace directory is `" + s.Dir + "`. Use the `exec` tool to run commands there.\n", nil
}
