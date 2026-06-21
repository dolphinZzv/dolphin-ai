package setup

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/config"
	"dolphin/internal/limit"
	"dolphin/internal/llm"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
)

type TransportsBootstrapper struct{}

func (b *TransportsBootstrapper) Name() string { return "transports" }
func (b *TransportsBootstrapper) Index() int   { return 120 }
func (b *TransportsBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Transports != nil {
		return nil
	}
	transportCfgs, _ := loadTransportConfigs(c.Config, c.Config.GetString("agent.name"))
	if hasTransportType(transportCfgs, "tui") && hasTransportType(transportCfgs, "stdio") {
		c.Logger.Fatal("tui and stdio transports cannot be enabled at the same time",
			zap.String("reason", "both require raw terminal mode"),
		)
	}
	for _, tc := range transportCfgs {
		tc.Config["logger"] = c.Logger
		// For TUI, inject the active model's temperature and a lookup
		// callback so the status bar can display temperature and refresh
		// it when the user switches models via /models use.
		if tc.Type == "tui" && c.LLMProvider != nil {
			activeModel, _ := tc.Config["model"].(string)
			tc.Config["temperature"] = lookupTemperature(c.LLMProvider, activeModel)
			tc.Config["temp_for"] = func(name string) float64 {
				return lookupTemperature(c.LLMProvider, name)
			}
			tc.Config["reasoning_effort"] = lookupReasoningEffort(c.LLMProvider, activeModel)
			tc.Config["reasoning_effort_for"] = func(name string) string {
				return lookupReasoningEffort(c.LLMProvider, name)
			}
			tc.Config["thinking"] = lookupThinking(c.LLMProvider, activeModel)
			tc.Config["thinking_for"] = func(name string) bool {
				return lookupThinking(c.LLMProvider, name)
			}
		}
		tio, err := transport.Build(ctx, tc.Type, tc.Config)
		if err != nil {
			c.Logger.Fatal("transport build failed", zap.String("type", tc.Type), zap.Error(err))
		}
		if sh, ok := tio.(interface {
			SetSessionManager(transport.SessionManager)
		}); ok {
			sh.SetSessionManager(c.SessionMgr)
		}
		if sm, ok := tio.(interface{ SetSessionMode(bool) }); ok {
			sm.SetSessionMode(c.Config.GetString("session.mode") == "shared")
		}
		if ss, ok := tio.(interface{ SetSession(*session.Session) }); ok {
			if s := c.SessionMgr.Active(); s != nil {
				ss.SetSession(s)
			}
		}
		if sl, ok := tio.(interface{ SetLimiter(*limit.Limiter) }); ok && c.Limit != nil {
			sl.SetLimiter(c.Limit)
		}
		c.AgentIO.RegisterTransport(tio.ID(), tio)
		c.Transports = append(c.Transports, tio)
		if sa, ok := tio.(interface{ SetAgentIO(*agentio.AgentIO) }); ok {
			sa.SetAgentIO(c.AgentIO)
		}

		// Register transport-specific MCP tools from Tools().
		for i, td := range tio.Tools() {
			srcName := fmt.Sprintf("%s_mcp_%d", tio.ID(), i)
			switch {
			case td.Executor != nil:
				if exec, ok := td.Executor.(tool.Executor); ok {
					c.ToolReg.AddNamedSource(srcName, exec)
					c.Logger.Info("registered transport built-in MCP source",
						zap.String("transport", tio.ID()),
					)
				}
			case td.URL != "":
				client := mcp.NewClient(td.URL)
				c.ToolReg.AddNamedSource(srcName, client)
				c.Logger.Info("registered transport MCP source",
					zap.String("transport", tio.ID()),
					zap.String("url", td.URL),
				)
			case td.Command != "":
				client, err := mcp.NewStdioClient(ctx, td.Command, td.Args)
				if err != nil {
					c.Logger.Warn("transport MCP stdio client failed",
						zap.String("transport", tio.ID()),
						zap.String("command", td.Command),
						zap.Error(err),
					)
					continue
				}
				c.ToolReg.AddNamedSource(srcName, client)
				c.Logger.Info("registered transport MCP source",
					zap.String("transport", tio.ID()),
					zap.String("command", td.Command),
				)
			}
		}
	}
	return nil
}

// loadTransportConfigs constructs transport configurations from config.
func loadTransportConfigs(cfg *config.Config, agentName string) ([]transportConfig, error) {
	var tcs []transportConfig
	hasExplicit := false
	for i := 0; ; i++ {
		typ := cfg.GetString("transport." + strconv.Itoa(i) + ".type")
		if typ == "" {
			break
		}
		if !cfg.GetBool("transport." + strconv.Itoa(i) + ".enabled") {
			continue
		}
		hasExplicit = true
		tcs = append(tcs, transportConfig{
			Type: typ,
			Config: map[string]any{
				"type":       typ,
				"agent_name": agentName,
			},
		})
	}
	if cfg.GetBool("dingtalk.enabled") {
		dingtalkClientID := cfg.GetString("dingtalk.client_id")
		dingtalkClientSecret := cfg.GetString("dingtalk.client_secret")
		dingtalkWebhook := cfg.GetString("dingtalk.webhook_url")
		dingtalkConfigured := dingtalkClientID != "" && dingtalkClientSecret != "" && dingtalkWebhook != ""
		if !hasTransportType(tcs, "dingtalk") && dingtalkConfigured {
			tcs = append(tcs, transportConfig{
				Type: "dingtalk",
				Config: map[string]any{
					"type":          "dingtalk",
					"agent_name":    agentName,
					"client_id":     dingtalkClientID,
					"client_secret": dingtalkClientSecret,
					"webhook_url":   dingtalkWebhook,
					"allow_users":   configListOrString(cfg, "dingtalk.allow_users"),
				},
			})
		}
	}
	if cfg.GetBool("email.enabled") {
		emailAddr := cfg.GetString("email.address")
		emailPass := cfg.GetString("email.password")
		emailKey := cfg.GetString("email.key")
		if !hasTransportType(tcs, "email") && emailAddr != "" && emailPass != "" {
			emailCfg := map[string]any{
				"type":          "email",
				"agent_name":    agentName,
				"email_address": emailAddr,
				"password":      emailPass,
				"imap_server":   cfg.GetString("email.imap_server"),
				"imap_port":     cfg.GetString("email.imap_port"),
				"smtp_server":   cfg.GetString("email.smtp_server"),
				"smtp_port":     cfg.GetString("email.smtp_port"),
				"key":           emailKey,
				"allow_senders": configListOrString(cfg, "email.allow_senders"),
			}
			tcs = append(tcs, transportConfig{Type: "email", Config: emailCfg})
		}
	}
	if cfg.GetBool("panda.enabled") {
		pandaServer := cfg.GetString("panda.server")
		pandaAccount := cfg.GetString("panda.account")
		pandaPassword := cfg.GetString("panda.password")
		if !hasTransportType(tcs, "panda") && pandaServer != "" && pandaAccount != "" && pandaPassword != "" {
			tcs = append(tcs, transportConfig{
				Type: "panda",
				Config: map[string]any{
					"type":        "panda",
					"agent_name":  agentName,
					"server":      pandaServer,
					"account":     pandaAccount,
					"password":    pandaPassword,
					"conv_id":     cfg.GetString("panda.conv_id"),
					"allow_users": configListOrString(cfg, "panda.allow_users"),
					"allow_convs": configListOrString(cfg, "panda.allow_convs"),
				},
			})
		}
	}
	if cfg.GetBool("a2a.enabled") {
		a2aAddr := cfg.GetString("a2a.addr")
		if !hasTransportType(tcs, "a2a") && a2aAddr != "" {
			tcs = append(tcs, transportConfig{
				Type: "a2a",
				Config: map[string]any{
					"type":        "a2a",
					"agent_name":  agentName,
					"addr":        a2aAddr,
					"name":        cfg.GetString("a2a.name"),
					"description": cfg.GetString("a2a.description"),
					"url":         cfg.GetString("a2a.url"),
					"version":     cfg.GetString("a2a.version"),
				},
			})
		}
	}
	if cfg.GetBool("wework.enabled") {
		botID := cfg.GetString("wework.bot_id")
		botSecret := cfg.GetString("wework.bot_secret")
		if botID == "" {
			botID = os.Getenv("WEWORK")
		}
		if botSecret == "" {
			botSecret = os.Getenv("WESecret")
		}
		if !hasTransportType(tcs, "wework") && botID != "" && botSecret != "" {
			tcs = append(tcs, transportConfig{
				Type: "wework",
				Config: map[string]any{
					"type":        "wework",
					"agent_name":  agentName,
					"bot_id":      botID,
					"bot_secret":  botSecret,
					"allow_users": configListOrString(cfg, "wework.allow_users"),
				},
			})
		}
	}
	if cfg.GetBool("tui.enabled") {
		if !hasTransportType(tcs, "tui") {
			tcs = append(tcs, transportConfig{
				Type: "tui",
				Config: map[string]any{
					"type":                  "tui",
					"agent_name":            agentName,
					"theme":                 cfg.GetString("tui.theme"),
					"model":                 cfg.GetString("llm.use"),
					"show_tools":            cfg.GetBool("tui.show_tools"),
					"show_thinking":         cfg.GetBool("tui.show_thinking"),
					"workmode":              cfg.GetString("agent.workmode"),
					"pool_size":             cfg.GetInt("agent.pool_size"),
					"tool_parallelism":      cfg.GetInt("agent.tool_parallelism"),
					"compaction.max_tokens": cfg.GetInt("compaction.max_tokens"),
				},
			})
		}
	}
	if !hasExplicit && !hasTransportType(tcs, "stdio") {
		stdioExplicitlyDisabled := configHasKey(cfg, "stdio.enabled") && !cfg.GetBool("stdio.enabled")
		if !stdioExplicitlyDisabled {
			tcs = append([]transportConfig{{Type: "stdio", Config: map[string]any{"type": "stdio"}}}, tcs...)
		}
	}
	return tcs, nil
}

func hasTransportType(tcs []transportConfig, typ string) bool {
	for _, tc := range tcs {
		if tc.Type == typ {
			return true
		}
	}
	return false
}

// lookupTemperature returns the configured temperature for the model,
// matching by exact name or by the short name (after a "provider/" prefix).
// Returns 0 if the model is not found or has no temperature set, which
// the TUI renders as "temp:0.0".
func lookupTemperature(provider llm.Provider, modelName string) float64 {
	if provider == nil || modelName == "" {
		return 0
	}
	models, err := provider.Models(context.Background())
	if err != nil {
		return 0
	}
	short := modelName
	if _, after, found := strings.Cut(modelName, "/"); found {
		short = after
	}
	for _, mc := range models {
		if mc.Name == modelName || mc.Name == short {
			return mc.Temperature
		}
	}
	return 0
}

// lookupReasoningEffort returns the configured reasoning_effort for the model.
func lookupReasoningEffort(provider llm.Provider, modelName string) string {
	if provider == nil || modelName == "" {
		return ""
	}
	models, err := provider.Models(context.Background())
	if err != nil {
		return ""
	}
	short := modelName
	if _, after, found := strings.Cut(modelName, "/"); found {
		short = after
	}
	for _, mc := range models {
		if mc.Name == modelName || mc.Name == short {
			return mc.ReasoningEffort
		}
	}
	return ""
}

// lookupThinking returns whether extended thinking is enabled for the model.
func lookupThinking(provider llm.Provider, modelName string) bool {
	if provider == nil || modelName == "" {
		return false
	}
	models, err := provider.Models(context.Background())
	if err != nil {
		return false
	}
	short := modelName
	if _, after, found := strings.Cut(modelName, "/"); found {
		short = after
	}
	for _, mc := range models {
		if mc.Name == modelName || mc.Name == short {
			return mc.Thinking
		}
	}
	return false
}

// configHasKey returns true if the config key exists (was explicitly set).
func configHasKey(cfg *config.Config, key string) bool {
	for _, k := range cfg.Keys() {
		if k == key {
			return true
		}
	}
	return false
}

type transportConfig struct {
	Type   string
	Config map[string]any
}
