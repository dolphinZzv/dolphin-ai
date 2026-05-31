package setup

import (
	"context"

	"dolphin/internal/logger"
)

type LoggerBootstrapper struct{}

func (b *LoggerBootstrapper) Name() string { return "logger" }
func (b *LoggerBootstrapper) Index() int   { return 10 }
func (b *LoggerBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Logger != nil {
		return nil
	}
	c.Logger = logger.New(c.Config)
	return nil
}
