package setup

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/pprof"
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

	shutdown, errc := pprof.Start(addr)
	c.PprofShutdown = shutdown

	// Watch for startup failures asynchronously (e.g. port already in use).
	go func() {
		for err := range errc {
			if err != nil {
				c.Logger.Error("pprof server error", zap.Error(err))
			}
		}
	}()

	c.Logger.Info("pprof server started", zap.String("addr", addr))
	return nil
}
