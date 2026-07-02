package setup

import (
	"context"

	"dolphin/internal/command"
	"dolphin/internal/scheduler"
	"dolphin/internal/tool"
)

type SchedulerBootstrapper struct{}

func (b *SchedulerBootstrapper) Name() string { return "scheduler" }
func (b *SchedulerBootstrapper) Index() int   { return 80 }
func (b *SchedulerBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Scheduler != nil {
		return nil
	}
	schedDir := c.Config.GetString("session.dir") + "/scheduler"
	c.Scheduler = scheduler.New(schedDir, c.Logger, c.Brain)
	tool.RegisterSchedulerTools(c.ToolReg, c.Scheduler)
	command.RegisterScheduler(c.CmdReg, c.Scheduler)
	return nil
}
