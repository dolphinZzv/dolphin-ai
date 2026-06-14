package setup

import (
	"context"

	"dolphin/internal/memory"
)

type MemoryBootstrapper struct{}

func (b *MemoryBootstrapper) Name() string { return "memory" }
func (b *MemoryBootstrapper) Index() int   { return 40 }
func (b *MemoryBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Mem != nil {
		return nil
	}
	window := c.Config.GetInt("session.window")
	c.Mem = memory.NewDroppingMemory(
		memory.NewFileMemory(c.SessionMgr),
		window,
	)
	return nil
}
