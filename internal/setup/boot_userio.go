package setup

import (
	"context"

	"dolphin/internal/userio"
)

type UserIOBootstrapper struct{}

func (b *UserIOBootstrapper) Name() string { return "userio" }
func (b *UserIOBootstrapper) Index() int   { return 100 }
func (b *UserIOBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.UserIO != nil {
		return nil
	}
	c.UserIO = userio.NewUserIO(c.AgentIO, c.CmdReg, c.SessionMgr)
	return nil
}
