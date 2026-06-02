package setup

import (
	"context"
	"strconv"

	"dolphin/internal/agentloop"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/mcp"
	"dolphin/internal/skill"
	"dolphin/internal/tool"

	"go.uber.org/zap"
)

type ToolsBootstrapper struct{}

func (b *ToolsBootstrapper) Name() string { return "tools" }
func (b *ToolsBootstrapper) Index() int   { return 60 }
func (b *ToolsBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.ToolReg != nil {
		return nil
	}

	c.ToolReg = tool.NewRegistry()
	loadMCPServers(c.Config, c.ToolReg, c.Logger)

	catalogEntries, _ := loadCatalogFromConfig(c.Config)
	catalog := tool.NewCatalog(catalogEntries)
	for name, mt := range tool.MetaHandler(catalog, c.ToolReg) {
		c.ToolReg.RegisterBuiltin(name, "MCP server discovery", mt.Schema, mt.Handler)
	}

	c.SkillStore = skill.NewFileStore(c.Config.GetString("brain.dir") + "/skills")
	c.CmdReg = command.NewRegistry(c.SessionMgr, c.SignalBus)
	tool.RegisterSkillTools(c.ToolReg, tool.SkillAdapter{Store: c.SkillStore})
	tool.RegisterSessionTools(c.ToolReg, c.SessionMgr)

	command.RegisterLang(c.CmdReg)
	command.RegisterMCP(c.CmdReg, c.ToolReg)
	command.RegisterSkills(c.CmdReg, c.SkillStore)
	command.RegisterModels(c.CmdReg, c.LLMProvider)
	command.RegisterContext(c.CmdReg, func(ctx context.Context) (string, error) {
		cbs := &agentloop.ContextBuilderStage{
			SkillStore: c.SkillStore,
			Brain:      c.Brain,
			Workspace:  c.Config.GetString("agent.workspace"),
		}
		return cbs.BuildSystemPrompt(ctx)
	})

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type mcpServerConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Enabled bool     `yaml:"enabled"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	URL     string   `yaml:"url"`
}

func loadMCPServers(cfg *config.Config, reg *tool.Registry, logger *zap.Logger) {
	builtinHandlers := tool.BuiltinMCPHandlers()
	builtinDescs := tool.BuiltinMCPDescriptions()
	builtinSchemas := tool.BuiltinMCPSchemas()

	for i := 0; ; i++ {
		prefix := "mcp_servers." + strconv.Itoa(i)
		name := cfg.GetString(prefix + ".name")
		if name == "" {
			break
		}

		if !cfg.GetBool(prefix + ".enabled") {
			continue
		}

		mtype := cfg.GetString(prefix + ".type")

		if mtype == "builtin" {
			handler, ok := builtinHandlers[name]
			if !ok {
				logger.Warn("unknown builtin MCP server", zap.String("name", name))
				continue
			}
			reg.RegisterBuiltin(name, builtinDescs[name], builtinSchemas[name], handler)
			logger.Info("registered builtin MCP server", zap.String("name", name))
			continue
		}

		switch mtype {
		case "url":
			url := cfg.GetString(prefix + ".url")
			if url == "" {
				logger.Warn("mcp server missing url", zap.String("name", name))
				continue
			}
			client := mcp.NewClient(url)
			defs, err := client.List(context.Background())
			if err != nil {
				logger.Warn("mcp server connect failed",
					zap.String("name", name),
					zap.String("url", url),
					zap.Error(err),
				)
				continue
			}
			reg.AddNamedSource(name, client)
			logger.Info("loaded MCP server",
				zap.String("name", name),
				zap.String("url", url),
				zap.Int("tools", len(defs)),
			)

		case "stdio":
			cmd := cfg.GetString(prefix + ".command")
			if cmd == "" {
				logger.Warn("mcp server missing command", zap.String("name", name))
				continue
			}
			var args []string
			for j := 0; ; j++ {
				a := cfg.GetString(prefix + ".args." + strconv.Itoa(j))
				if a == "" {
					break
				}
				args = append(args, a)
			}
			client, err := mcp.NewStdioClient(context.Background(), cmd, args)
			if err != nil {
				logger.Warn("mcp stdio server start failed",
					zap.String("name", name),
					zap.String("command", cmd),
					zap.Error(err),
				)
				continue
			}
			defs, err := client.List(context.Background())
			if err != nil {
				logger.Warn("mcp stdio server list failed",
					zap.String("name", name),
					zap.Error(err),
				)
				client.Close()
				continue
			}
			reg.AddNamedSource(name, client)
			logger.Info("loaded stdio MCP server",
				zap.String("name", name),
				zap.String("command", cmd),
				zap.Int("tools", len(defs)),
			)
		}
	}
}

func loadCatalogFromConfig(cfg *config.Config) ([]tool.CatalogEntry, error) {
	var entries []tool.CatalogEntry
	for i := 0; ; i++ {
		name := cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".name")
		if name == "" {
			break
		}
		entries = append(entries, tool.CatalogEntry{
			Name:        name,
			Description: cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".description"),
			URL:         cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".url"),
		})
	}
	return entries, nil
}

func configListOrString(cfg *config.Config, key string) string {
	if v := cfg.GetString(key); v != "" {
		return v
	}
	var parts []string
	for i := 0; ; i++ {
		v := cfg.GetString(key + "." + strconv.Itoa(i))
		if v == "" {
			break
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "," + p
	}
	return result
}
