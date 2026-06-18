package setup

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"dolphin/internal/cli"
	appctx "dolphin/internal/context"
	"dolphin/internal/tool"
)

type CLIBootstrapper struct{}

func (b *CLIBootstrapper) Name() string { return "cli" }
func (b *CLIBootstrapper) Index() int   { return 85 }

func (b *CLIBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	raw := configListOrString(c.Config, "agent.bin")
	if raw == "" {
		return nil
	}
	dirs := strings.Split(raw, ",")
	for i := range dirs {
		dirs[i] = strings.TrimSpace(dirs[i])
	}

	clis := cli.Scan(dirs, c.Logger)
	if len(clis) == 0 {
		return nil
	}

	c.ContextSections = append(c.ContextSections, &appctx.CliSection{CLIs: clis, Logger: c.Logger})

	// Re-register shell tool with bin dirs in PATH.
	handlers := tool.BuiltinMCPHandlers(dirs)
	descs := tool.BuiltinMCPDescriptions()
	schemas := tool.BuiltinMCPSchemas()
	if h, ok := handlers["shell"]; ok {
		c.ToolReg.RegisterBuiltin("shell", descs["shell"], schemas["shell"], h)
	}

	c.Logger.Info("CLI bootstrapper loaded",
		zap.Int("cli_count", len(clis)), zap.String("dirs", raw))
	return nil
}
