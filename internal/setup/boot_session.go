package setup

import (
	"context"

	"dolphin/internal/session"
)

type SessionBootstrapper struct{}

func (b *SessionBootstrapper) Name() string { return "session" }
func (b *SessionBootstrapper) Index() int   { return 30 }
func (b *SessionBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.SessionMgr != nil {
		return nil
	}
	mgr := session.NewManager(c.Config.GetString("session.dir"))
	mgr.LoadActive(ctx)
	c.SessionMgr = mgr
	return nil
}
