package context

import stdctx "context"

// BrainIndexReader provides the brain index content to inject into system prompt.
type BrainIndexReader interface {
	ReadIndex(ctx stdctx.Context) (string, error)
}

// Brain injects brain index.
type Brain struct {
	Reader BrainIndexReader
}

func (s *Brain) Name() string { return "brain" }
func (s *Brain) Index() int   { return 5 }
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
