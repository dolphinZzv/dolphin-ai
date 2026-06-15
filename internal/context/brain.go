package context

import stdctx "context"

// BrainIndexReader provides the brain tree and file contents.
type BrainIndexReader interface {
	Tree() (string, error)
}

// Brain injects a tree view of the brain directory so the LLM can
// decide which files to read via the brain_read tool.
type Brain struct {
	Reader BrainIndexReader
}

func (s *Brain) Name() string { return "brain" }
func (s *Brain) Index() int   { return 5 }
func (s *Brain) BuildContent(ctx stdctx.Context) (string, error) {
	if s.Reader == nil {
		return "", nil
	}
	tree, err := s.Reader.Tree()
	if err != nil || tree == "" {
		return "", nil
	}
	return "## Brain Index\nThe following is my long-term knowledge directory. Use brain_read to load files you need, brain_list to search by keyword.\n\n" + tree, nil
}
