package registry

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe container for CommandSpecs.
type Registry struct {
	mu      sync.RWMutex
	specs   map[string]*CommandSpec
	ordered []string
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{
		specs: make(map[string]*CommandSpec),
	}
}

// Register adds a spec. Panics on duplicate name.
func (r *Registry) Register(spec *CommandSpec) {
	if spec == nil || spec.Cobra == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	name := spec.Cobra.Name()
	if _, exists := r.specs[name]; exists {
		panic(fmt.Sprintf("registry: duplicate command %q", name))
	}
	r.specs[name] = spec
	r.ordered = append(r.ordered, name)
}

// Get looks up a spec by command name. Returns nil when not found.
func (r *Registry) Get(name string) *CommandSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.specs[name]
}

// List returns all registered specs in insertion order.
func (r *Registry) List() []*CommandSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*CommandSpec, 0, len(r.ordered))
	for _, name := range r.ordered {
		out = append(out, r.specs[name])
	}
	return out
}

// ListByCategory groups specs by their Category.
func (r *Registry) ListByCategory() map[Category][]*CommandSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[Category][]*CommandSpec)
	for _, name := range r.ordered {
		s := r.specs[name]
		out[s.Category] = append(out[s.Category], s)
	}
	return out
}
