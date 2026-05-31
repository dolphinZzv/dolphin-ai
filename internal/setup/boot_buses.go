package setup

import (
	"context"

	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/signal"
)

type BusesBootstrapper struct{}

func (b *BusesBootstrapper) Name() string { return "buses" }
func (b *BusesBootstrapper) Index() int   { return 20 }
func (b *BusesBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.EventBus != nil {
		return nil
	}
	c.EventBus = event.NewBus()
	c.HookReg = hook.NewRegistry()
	c.SignalBus = signal.NewBus()
	return nil
}
