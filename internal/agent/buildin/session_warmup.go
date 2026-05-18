package buildin

import (
	"context"
	_ "embed"

	"dolphin/internal/event"
)

//go:embed session_warmup.md
var warmupPrompt string

type warmupAgent struct{}

func (a *warmupAgent) Name() string   { return "$buildin.warmup" }
func (a *warmupAgent) Prompt() string { return warmupPrompt }

func (a *warmupAgent) Init(ctx context.Context, handle *AgentHandle) {
	handle.Subscribe(event.TypeSessionCreated, func(ctx context.Context, evt event.Event) {
		handle.DispatchTask(ctx, a.Name(), string(evt.Type), a.Prompt())
	})
}

func init() { Register(&warmupAgent{}) }
