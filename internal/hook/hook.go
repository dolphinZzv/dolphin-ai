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

// DispatchCheck dispatches to all handlers and returns the first error.
// Used for pre-checks where handlers can block an operation.
func (r *Registry) DispatchCheck(ctx context.Context, e event.Event) error {
	for _, h := range r.handlers {
		if err := h.Handle(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// LLMRequestHook is called before an LLM request is sent.
// model and apiType identify the target model. The req pointer can be
// modified to rewrite the request.
type LLMRequestHook func(req any, model, apiType string)

var llmHooks []LLMRequestHook

// RegisterLLMRequestHook registers a hook that rewrites LLM requests.
func RegisterLLMRequestHook(h LLMRequestHook) {
	llmHooks = append(llmHooks, h)
}

// DispatchLLMRequest runs all registered LLM request hooks.
func DispatchLLMRequest(req any, model, apiType string) {
	for _, h := range llmHooks {
		h(req, model, apiType)
	}
}
