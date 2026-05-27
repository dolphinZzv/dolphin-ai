package buildin

import (
	"context"
	_ "embed"
	"time"

	"dolphin/internal/agent/buildin"
	"dolphin/internal/config"
	"dolphin/internal/event"
)

//go:embed health.md
var healthPrompt string

type healthAgent struct{}

func (a *healthAgent) Name() string   { return "$buildin.health" }
func (a *healthAgent) Prompt() string { return healthPrompt }

func (a *healthAgent) Init(ctx context.Context, handle *buildin.AgentHandle) {
	var lastFired time.Time

	handle.Subscribe(event.TypeHeartbeat, func(ctx context.Context, evt event.Event) {
		if time.Since(lastFired) < healthDebounce(handle.Cfg) {
			return
		}
		lastFired = time.Now()

		handle.DispatchTask(ctx, a.Name(), string(evt.Type), a.Prompt())
	})
}

func init() { buildin.Register(&healthAgent{}) }

func healthDebounce(cfg *config.Config) time.Duration {
	if cfg == nil {
		return 30 * time.Second
	}
	s := cfg.Health.Debounce
	if s == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}
