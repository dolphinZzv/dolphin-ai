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
	dir := c.Config.GetString("memory.dir")
	window := c.Config.GetInt("memory.window")
	c.Mem = memory.NewDroppingMemory(
		memory.NewFileMemory(dir, window),
		window,
	)
	return nil
}
