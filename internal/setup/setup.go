package setup

import (
	"context"
	"fmt"
	"sort"
)

// Bootstrapper initializes a component. Implementations register themselves
// via Registry and are run in Index order.
type Bootstrapper interface {
	Name() string
	Index() int
	Bootstrap(ctx context.Context, c *Context) error
}

// Registry collects bootstrappers and runs them in Index order.
type Registry struct {
	bootstrappers []Bootstrapper
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(b Bootstrapper) {
	r.bootstrappers = append(r.bootstrappers, b)
}

// Bootstrap runs all registered bootstrappers sorted by Index.
func (r *Registry) Bootstrap(ctx context.Context, c *Context) error {
	ordered := make([]Bootstrapper, len(r.bootstrappers))
	copy(ordered, r.bootstrappers)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Index() < ordered[j].Index()
	})
	for _, b := range ordered {
		if err := b.Bootstrap(ctx, c); err != nil {
			return fmt.Errorf("setup %s: %w", b.Name(), err)
		}
	}
	return nil
}
