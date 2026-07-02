package setup

import (
	"context"
	"time"

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

	var base memory.Memory
	switch c.Config.GetString("session.type") {
	case "sqlite":
		dir := c.Config.GetString("session.dir")
		db, err := memory.NewSQLiteMemory(dir + "/memory.db")
		if err != nil {
			return err
		}
		base = db
	case "wal":
		dir := c.Config.GetString("session.dir")
		retention := c.Config.GetDuration("session.wal_retention")
		if retention <= 0 {
			retention = 30 * 24 * time.Hour
		}
		wm, err := memory.NewWALMemory(dir, retention, c.Config.GetInt("session.wal_keep_turns"))
		if err != nil {
			return err
		}
		base = wm
	default:
		base = memory.NewFileMemory(c.SessionMgr)
	}

	c.Mem = memory.NewDroppingMemory(base, window)
	return nil
}
