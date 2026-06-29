package setup

import (
	"context"
	"strconv"

	"go.uber.org/zap"

	"dolphin/internal/command"
	"dolphin/internal/config"
	appctx "dolphin/internal/context"
	"dolphin/internal/mcp"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	"dolphin/internal/types"
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
	metaDescs := map[string]string{
		"mcp_search": "搜索 MCP 服务器，返回匹配结果列表",
		"mcp_load":   "通过 URL 加载 MCP 服务器并建立连接",
	}
	for name, mt := range tool.MetaHandler(catalog, c.ToolReg) {
		desc := metaDescs[name]
		if desc == "" {
			desc = name
		}
		c.ToolReg.RegisterBuiltin(name, desc, mt.Schema, mt.Handler)
	}

	skillStore, err := skill.NewFileStore(c.Config.GetString("brain.dir") + "/skills")
	if err != nil {
		return err
	}
	c.SkillStore = skillStore
	c.CmdReg = command.NewRegistry(c.SessionMgr, c.SignalBus)
	tool.RegisterSkillTools(c.ToolReg, tool.SkillAdapter{Store: c.SkillStore})
	tool.RegisterSessionTools(c.ToolReg, c.SessionMgr)

	command.RegisterLang(c.CmdReg)
	command.RegisterMCP(c.CmdReg, c.ToolReg)
	command.RegisterSkills(c.CmdReg, c.SkillStore)
	command.RegisterModels(c.CmdReg, c.LLMProvider)
	command.RegisterQueue(c.CmdReg)
	command.RegisterLimit(c.CmdReg, c.Limit)
	command.RegisterSessionStatus(c.CmdReg, c.SessionMgr, c.Mem, c.Config.GetString("session.mode"), c.LLMProvider)
	command.RegisterContext(c.CmdReg, func() *appctx.Registry {
		return c.ContextReg
	})
	command.RegisterConfig(c.CmdReg)
	command.RegisterBrain(c.CmdReg, c.Brain)
	command.RegisterCompaction(
		c.CmdReg,
		c.LLMProvider,
		c.Mem,
		c.Config.GetInt("compaction.max_tokens"),
		c.Config.GetInt("compaction.keep_rounds"),
		c.Config.GetInt("compaction.summary_max_tokens"),
		c.Config.GetInt("compaction.token_ratio"),
		c.Config.GetString("compaction.model"),
		c.EventBus,
		c.Logger,
		c.SessionMgr,
	)

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func loadMCPServers(cfg *config.Config, reg *tool.Registry, logger *zap.Logger) {
	builtinHandlers := tool.BuiltinMCPHandlers(nil)
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
		case "url", "http":
			url := cfg.GetString(prefix + ".url")
			headers := cfg.GetStringMap(prefix + ".headers")
			if url == "" {
				logger.Warn("mcp server missing url", zap.String("name", name))
				continue
			}
			ac := mcp.NewAsyncClient(func(ctx context.Context) (mcp.ClientExecutor, []types.ToolDef, error) {
				client := mcp.NewClient(url)
				if len(headers) > 0 {
					client.SetHeaders(headers)
				}
				defs, err := client.List(ctx)
				if err != nil {
					return nil, nil, err
				}
				return client, defs, nil
			})
			ac.SetOnConnect(func(count int) {
				logger.Info("async MCP server connected",
					zap.String("name", name),
					zap.String("url", url),
					zap.Int("tools", count),
				)
			})
			reg.AddNamedSource(name, ac)
			logger.Info("registered async MCP server (url)",
				zap.String("name", name),
				zap.String("url", url),
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
			ac := mcp.NewAsyncClient(func(ctx context.Context) (mcp.ClientExecutor, []types.ToolDef, error) {
				client, err := mcp.NewStdioClient(ctx, cmd, args)
				if err != nil {
					return nil, nil, err
				}
				defs, err := client.List(ctx)
				if err != nil {
					client.Close()
					return nil, nil, err
				}
				return client, defs, nil
			})
			ac.SetOnConnect(func(count int) {
				logger.Info("async stdio MCP server connected",
					zap.String("name", name),
					zap.String("command", cmd),
					zap.Int("tools", count),
				)
			})
			reg.AddNamedSource(name, ac)
			logger.Info("registered async stdio MCP server",
				zap.String("name", name),
				zap.String("command", cmd),
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
