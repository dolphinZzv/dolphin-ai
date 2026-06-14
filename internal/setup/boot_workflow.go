package setup

import (
	"context"

	"dolphin/internal/workflow"
)

type WorkflowBootstrapper struct{}

func (b *WorkflowBootstrapper) Name() string { return "workflow" }
func (b *WorkflowBootstrapper) Index() int   { return 91 }

func (b *WorkflowBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	engine := workflow.NewEngine(
		c.ToolReg,
		c.LLMProvider,
		c.EventBus,
		c.Logger,

		c.AgentIO,
		c.Config,
	)

	// Register tools.
	workflow.RegisterTools(c.ToolReg, engine, c.AgentIO, c.Logger)

	if c.Brain != nil {
		engine.SetBrainDir(c.Brain.Dir())
	}

	c.WorkflowEngine = engine
	return nil
}
