package buildin

import (
	"context"
	_ "embed"
	"time"

	"dolphin/internal/event"
)

//go:embed health.md
var healthPrompt string

type healthAgent struct{}

func (a *healthAgent) Name() string   { return "$buildin.health" }
func (a *healthAgent) Prompt() string { return healthPrompt }

func (a *healthAgent) Init(ctx context.Context, handle *AgentHandle) {
	var lastFired time.Time

	handle.Subscribe(event.TypeHeartbeat, func(ctx context.Context, evt event.Event) {
		if time.Since(lastFired) < 30*time.Second {
			return
		}
		lastFired = time.Now()

		handle.DispatchTask(ctx, a.Name(), string(evt.Type), a.Prompt())
	})
}

func init() { Register(&healthAgent{}) }
