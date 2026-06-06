package setup

import (
	"context"
	"fmt"
	"os"
	"time"

	"dolphin/internal/brain"
	"dolphin/internal/watcher"
	"dolphin/internal/command"
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
	tool.RegisterCommandTools(c.ToolReg, br)
	tool.RegisterScriptTools(c.ToolReg, br)
	command.RegisterCommands(c.CmdReg, br)
	command.RegisterScripts(c.CmdReg, br)
	command.RegisterSubscriptionCmd(c.CmdReg, br)
	c.Brain = br

	if c.SkillStore != nil {
		skill.SeedDefaults(ctx, c.SkillStore)
	}

	// Set up subscription engine and file watchers.
	// Always watch the brain directory.
	watchers := []*watcher.Watcher{
		watcher.NewWatcher(brainDir, c.EventBus, 5*time.Second),
	}
	// Also watch the workspace root by default for SOUL.md etc.
	workspace := c.Config.GetString("agent.workspace")
	if workspace != "" && workspace != brainDir {
		watchers = append(watchers, watcher.NewWatcher(workspace, c.EventBus, 5*time.Second))
	}
	// Watch additional paths from config.
	for i := 0; ; i++ {
		key := fmt.Sprintf("watch.paths.%d", i)
		p := c.Config.GetString(key)
		if p == "" {
			break
		}
		watchers = append(watchers, watcher.NewWatcher(p, c.EventBus, 5*time.Second))
	}
	engine := brain.NewSubscriptionEngine(br, c.EventBus, c.Logger)
	c.Watchers = watchers
	c.SubscriptionEngine = engine
	tool.RegisterSubscriptionTools(c.ToolReg, br)
	brain.SeedSubscriptions(ctx, br)
	return nil
}
