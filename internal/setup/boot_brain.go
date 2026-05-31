package setup

import (
	"context"
	"fmt"
	"os"

	"dolphin/internal/brain"
	"dolphin/internal/skill"
	"dolphin/internal/tool"

	"go.uber.org/zap"
)

type BrainBootstrapper struct{}

func (b *BrainBootstrapper) Name() string { return "brain" }
func (b *BrainBootstrapper) Index() int   { return 70 }
func (b *BrainBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Brain != nil {
		return nil
	}
	brainDir := c.Config.GetString("brain.dir")
	br := brain.New(brainDir)
	if br.IsInitialized() {
		c.Logger.Info("brain already initialized", zap.String("dir", brainDir))
	} else {
		c.Logger.Info("brain not initialized, creating", zap.String("dir", brainDir))
	}
	if err := br.Init(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "brain: init failed: %v\n", err)
		c.Logger.Fatal("brain init failed", zap.String("dir", brainDir), zap.Error(err))
	}
	fmt.Fprintf(os.Stdout, "brain: %s (git repo)\n", brainDir)
	tool.RegisterBrainTools(c.ToolReg, br)
	c.Brain = br

	if c.SkillStore != nil {
		skill.SeedDefaults(ctx, c.SkillStore)
	}
	return nil
}
