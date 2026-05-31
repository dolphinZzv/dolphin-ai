package context

import stdctx "context"

// Transport injects transport-specific context.
type Transport struct {
	ContextFunc func() string
}

func (s *Transport) Name() string { return "transport" }
func (s *Transport) Index() int   { return 3 }
func (s *Transport) BuildContent(_ stdctx.Context) (string, error) {
	ctx := s.ContextFunc()
	if ctx == "" {
		return "", nil
	}
	return "## Transport Context\n" + ctx + "\n", nil
}
