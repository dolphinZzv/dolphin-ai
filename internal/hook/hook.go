package hook

import (
	"context"

	"dolphin/internal/event"
)

// Handler processes events as they pass through the hook system.
type Handler interface {
	Name() string
	Handle(ctx context.Context, e event.Event) error
}

// Registry manages hook handlers that process events.
type Registry struct {
	handlers []Handler
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(h Handler) {
	r.handlers = append(r.handlers, h)
}

func (r *Registry) Dispatch(ctx context.Context, e event.Event) {
	for _, h := range r.handlers {
		if err := h.Handle(ctx, e); err != nil {
			continue
		}
	}
}
