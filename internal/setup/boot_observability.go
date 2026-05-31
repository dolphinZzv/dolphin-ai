package setup

import (
	"context"

	"dolphin/internal/event"
	"dolphin/internal/observability"
)

type ObservabilityBootstrapper struct{}

func (b *ObservabilityBootstrapper) Name() string { return "observability" }
func (b *ObservabilityBootstrapper) Index() int   { return 110 }
func (b *ObservabilityBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.OtelShutdown != nil {
		return nil
	}
	c.OtelShutdown = observability.BuildObservability(c.Config, c.HookReg, c.Logger)

	c.EventBus.Subscribe(event.NewLogHandler(c.Logger))
	c.EventBus.Subscribe(func(ctx context.Context, e event.Event) {
		c.HookReg.Dispatch(ctx, e)
	})
	return nil
}
