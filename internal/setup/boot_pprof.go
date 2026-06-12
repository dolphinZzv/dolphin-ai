package setup

import (
	"context"

	"dolphin/internal/pprof"

	"go.uber.org/zap"
)

type PprofBootstrapper struct{}

func (b *PprofBootstrapper) Name() string { return "pprof" }
func (b *PprofBootstrapper) Index() int   { return 111 }

func (b *PprofBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.PprofShutdown != nil {
		return nil
	}

	if !c.Config.GetBool("pprof.enabled") {
		return nil
	}

	addr := c.Config.GetString("pprof.addr")
	if addr == "" {
		addr = "127.0.0.1:6060"
	}

	c.PprofShutdown = pprof.Start(addr)
	c.Logger.Info("pprof server started", zap.String("addr", addr))
	return nil
}
