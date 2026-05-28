// Package cmd provides the CLI commands for dolphin.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"dolphin/internal/agent"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/diary"
	"dolphin/internal/event"
	"dolphin/internal/health"
	"dolphin/internal/hook"
	"dolphin/internal/hook/telemetry"
	"dolphin/internal/i18n"
	"dolphin/internal/logger"
	"dolphin/internal/mcp"
	"dolphin/internal/mcp/a2a"
	"dolphin/internal/mcp/cdp"
	"dolphin/internal/mcp/email"
	"dolphin/internal/mcp/llm"
	mcpshell "dolphin/internal/mcp/shell"
	"dolphin/internal/mcp/webhook"
	"dolphin/internal/mcp/webhost"
	"dolphin/internal/mcp/websearch"
	"dolphin/internal/metrics"
	"dolphin/internal/plugin"
	"dolphin/internal/registry"
	"dolphin/internal/resource"
	"dolphin/internal/scheduler"
	scope "dolphin/internal/scope"
	servermqtt "dolphin/internal/server/mqtt"
	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/subsystem"
	workflowpkg "dolphin/internal/subsystem/workflow"
	"dolphin/internal/transport"
	"dolphin/internal/update"

	_ "dolphin/internal/transport/a2a"
	_ "dolphin/internal/transport/dingtalk"
	_ "dolphin/internal/transport/email"
	_ "dolphin/internal/transport/mqtt"
	_ "dolphin/internal/transport/ssh"
	_ "dolphin/internal/transport/stdio"

	appctx "dolphin/internal/context"

	"dolphin/internal/actor"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	_ "net/http/pprof"
)

var (
	cfgFile    string
	verbose    bool
	quiet      bool
	Version    = "dev"
	BuildTime  = "unknown"
	CommitHash = "unknown"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     i18n.TL(i18n.KeyCmdDolphinUse),
		Short:   i18n.TL(i18n.KeyCmdDolphinShort),
		Long:    i18n.TL(i18n.KeyCmdDolphinLong),
		RunE:    runAgent,
		Version: Version,
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", i18n.TL(i18n.KeyFlagConfig))
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, i18n.TL(i18n.KeyFlagVerbose))
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, i18n.TL(i18n.KeyFlagQuiet))
	cmd.SetVersionTemplate("dolphin {{.Version}}\n")

	cmd.AddCommand(NewResetCmd())
	cmd.AddCommand(NewNewCmd())
	cmd.AddCommand(NewUpdateCmd())
	cmd.AddCommand(NewInstallCmd())
	cmd.AddCommand(NewInitCmd())
	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewSessionsCmd())
	cmd.AddCommand(NewConfigCmd())
	cmd.AddCommand(NewDoctorCmd())
	cmd.AddCommand(NewCompletionCmd())
	cmd.AddCommand(NewSkillsCmd())
	cmd.AddCommand(NewMCPCmd())
	cmd.AddCommand(NewAgentCmd())
	cmd.AddCommand(NewWorkflowCmd())

	return cmd
}

func runAgent(cmd *cobra.Command, args []string) error {
	// Create config manager and load
	mgr := config.NewManager(cfgFile)
	if err := mgr.Load(); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := mgr.Get()

	// Apply language config override (empty = auto-detect from env)
	if cfg.Language != "" {
		switch cfg.Language {
		case "en":
			i18n.SetLang(i18n.EN)
		case "zh":
			i18n.SetLang(i18n.ZH)
		default:
			zap.S().Warnw("unsupported language, falling back to auto-detect", "language", cfg.Language)
		}
	}

	// Apply --verbose/--quiet log level override
	switch {
	case verbose:
		cfg.Log.Level = "debug"
	case quiet:
		cfg.Log.Level = "error"
	}

	// Setup logging
	setupLogging(cfg)
	zap.S().Infow("config loaded", "session_dir", config.SessionsDir())

	// Check LLM configuration — warn if no API key is set
	if !cfg.LLMConfigured() {
		warnNoLLM(cfg)
	}

	// Init session manager
	sessMgr := session.NewManager(config.SessionsDir())
	if err := sessMgr.EnsureDir(); err != nil {
		return fmt.Errorf("session dir: %w", err)
	}
	defer sessMgr.Cleanup()

	// Init MCP tool registry
	toolRegistry := mcp.NewRegistry(cfg)

	// Register built-in tools (dynamically managed for hot-reload)
	toolRegistry.RegisterManagedTool("shell", func(cfg *config.Config) mcp.Tool { return mcpshell.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.Shell.Enabled })
	toolRegistry.RegisterManagedTool("read_process_output", func(cfg *config.Config) mcp.Tool { return mcpshell.NewProcessReaderTool() }, func(cfg *config.Config) bool { return cfg.MCP.Shell.Enabled })
	var cdpTool *cdp.Tool
	toolRegistry.RegisterManagedTool("cdp", func(cfg *config.Config) mcp.Tool {
		cdpTool = cdp.New(cfg)
		return cdpTool
	}, func(cfg *config.Config) bool { return cfg.MCP.CDP.Enabled })
	toolRegistry.RegisterManagedTool("email", func(cfg *config.Config) mcp.Tool { return email.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.Email.Enabled && cfg.Transport.Email.Username != "" })
	toolRegistry.RegisterManagedTool("webhook", func(cfg *config.Config) mcp.Tool { return webhook.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.Webhook.Enabled })
	toolRegistry.RegisterManagedTool("webhost", func(cfg *config.Config) mcp.Tool { return webhost.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.Webhost.Enabled })
	toolRegistry.RegisterManagedTool("web_search", func(cfg *config.Config) mcp.Tool { return websearch.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.WebSearch.Enabled })
	toolRegistry.RegisterManagedTool("llm", func(cfg *config.Config) mcp.Tool { return llm.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.LLM.Enabled })
	toolRegistry.RegisterManagedTool("a2a_send", func(cfg *config.Config) mcp.Tool { return a2a.New(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.A2A.Enabled })
	toolRegistry.RegisterManagedTool("a2a_list", func(cfg *config.Config) mcp.Tool { return a2a.NewListTool(cfg) }, func(cfg *config.Config) bool { return cfg.MCP.A2A.Enabled })
	zap.S().Infow("built-in tools registered dynamically")

	// Load external MCP servers — individual failures are non-fatal.
	if len(cfg.MCP.Servers) > 0 {
		toolRegistry.LoadServers(context.Background())
		defer toolRegistry.CloseServers()
	}

	tools := toolRegistry.List()
	zap.S().Infow("total mcp tools available", "count", len(tools))
	if cfg.Log.Level == "debug" {
		for _, t := range tools {
			zap.S().Debugw("mcp tool", "name", t.Name, "source", t.Source, "desc", t.Description)
		}
	}

	// Check for agents directory to decide coordinator vs single-agent mode
	agentsDir := filepath.Join(".dolphin", "agents")
	_, coordErr := os.Stat(agentsDir)
	hasAgents := coordErr == nil

	// Pre-load user-created agent definitions
	var agentDefs map[string]*agent.AgentDef
	if hasAgents {
		var err error
		agentDefs, err = agent.LoadAgentDefs(agentsDir)
		if err != nil {
			return fmt.Errorf("load agent defs: %w", err)
		}
		zap.S().Infow("coordinator mode enabled", "agents_dir", agentsDir, "count", len(agentDefs))
	} else {
		zap.S().Infow("no agents directory, using single-agent mode", "dir", agentsDir)
	}

	// Initialize skill and command managers
	skillMgr := initSkillManager(cfg)
	cmdMgr := initCommandManager(cfg)
	// Initialize workflow manager and register as subsystem
	wfmr := initWorkflowManager(cfg)
	subsystem.Register(wfmr)

	// Initialize cron task manager
	cronMgr := initCronManager(cfg)

	// Init plugin system: hooks (sync) + events (async)
	hooks := hook.NewRegistry()

	// Init OpenTelemetry tracing
	cfg.Telemetry.ServiceVersion = fmt.Sprintf("%s (%s)", Version, CommitHash)
	if err := telemetry.Init(context.Background(), cfg.Telemetry); err != nil {
		zap.S().Warnw("telemetry init failed, continuing without tracing", "error", err)
	} else if cfg.Telemetry.Enabled {
		telemetry.RegisterHooks(hooks)
		if cfg.Telemetry.LogsEnabled {
			telemetry.BridgeZap()
		}
	}

	bus := event.NewEventBus(256)
	mgr.Subscribe(bus)
	pm := plugin.NewManager(hooks, bus)
	toolRegistry.SetEventBus(bus)

	// Subscribe remaining global components for config hot-reload.
	mgr.Subscribe(toolRegistry)
	mgr.Subscribe(logLevelSubscriber{})
	if cronMgr != nil {
		mgr.Subscribe(cronMgr)
	}

	// Configure webhook delivery
	if cfg.Plugins.WebhookURL != "" {
		eventTypes := make([]event.Type, len(cfg.Plugins.WebhookEvents))
		for i, et := range cfg.Plugins.WebhookEvents {
			eventTypes[i] = event.Type(et)
		}
		if len(eventTypes) == 0 {
			eventTypes = []event.Type{"*"}
		}
		bus.SetWebhook(cfg.Plugins.WebhookURL, eventTypes)
		zap.S().Infow("webhook delivery configured", "url", cfg.Plugins.WebhookURL, "events", cfg.Plugins.WebhookEvents)
	}

	// Load script plugins from filesystem
	if cfg.Plugins.Enabled && cfg.Plugins.Dir != "" {
		if err := pm.LoadScripts(cfg.Plugins.Dir); err != nil {
			zap.S().Warnw("failed to load script plugins", "error", err)
		}
	}
	pm.Activate()

	// Load scope definitions for file-path-based agent routing
	scopeMgr, err := scope.LoadScopes(filepath.Join(".dolphin", "agents", "scopes.yaml"))
	if err != nil {
		zap.S().Warnw("failed to load scopes, continuing without scope routing", "error", err)
		scopeMgr = nil
	}
	if scopeMgr != nil && len(scopeMgr.Warnings) > 0 {
		for _, w := range scopeMgr.Warnings {
			zap.S().Warnw("scope validation warning", "detail", w)
		}
	}

	// Factory: creates a new coordinator per transport connection
	newCoordinator := func() *agent.Coordinator {
		cfg := mgr.Get()
		agt := agent.New(cfg, sessMgr, toolRegistry)
		agt.SetVersion(Version)
		agt.SetBuildTime(BuildTime)
		agt.SetCommitHash(CommitHash)
		agt.SetHooks(hooks)
		agt.SetEventBus(bus)
		agt.SetHeartbeatInterval(cfg.Plugins.HeartbeatTurns)
		mgr.Subscribe(agt)
		poolCfg := agent.NewPoolConfigFromConfig(cfg.Pool)
		pool := agent.NewAgentPool(context.Background(), poolCfg)
		if hasAgents {
			for name, def := range agentDefs {
				pool.Add(name, def, agent.AgentUser, agt, toolRegistry)
			}
		}
		// Register scope agents from scopes.yaml
		if scopeMgr != nil {
			for _, s := range scopeMgr.Scopes() {
				// Check name conflict with existing agents
				conflict := false
				for _, a := range pool.List() {
					if a.Name == s.Name {
						zap.S().Errorw("scope agent name conflicts with existing agent, skipping",
							"scope", s.Name, "existing_kind", a.Kind)
						conflict = true
						break
					}
				}
				if conflict {
					continue
				}
				roleText := "## Role\n" + s.Role
				if s.Context != "" {
					roleText += "\n\n## Additional Context\n" + s.Context
				}
				def := &agent.AgentDef{
					Name:      s.Name,
					Role:      roleText,
					Tools:     s.Tools,
					Skills:    s.Skills,
					Workflows: s.Workflows,
					Timeout:   s.Timeout,
				}
				if len(s.Tools) == 0 {
					zap.S().Warnw("scope agent has no explicit tools, all MCP tools will be available",
						"scope", s.Name)
				}
				pool.Add(s.Name, def, agent.AgentScope, agt, toolRegistry)
			}
			zap.S().Infow("scope agents registered", "count", len(scopeMgr.Scopes()))
		}
		coord := agent.NewCoordinator(agt, pool)
		coord.SetSkillManager(skillMgr)
		coord.SetCommandManager(cmdMgr)

		// Register skills and commands in unified registry (needs manager set).
		coord.RegisterCommandSpec(&registry.CommandSpec{
			Cobra:    skill.SkillsCommandWithManager(skillMgr),
			Category: registry.CatSkills,
		})
		coord.RegisterCommandSpec(&registry.CommandSpec{
			Cobra:    command.CommandsCommandWithManager(cmdMgr),
			Category: registry.CatCommands,
		})

		coord.SetWorkflowManager(wfmr)
		coord.SetCronManager(cronMgr)
		// Wire /reload command to trigger config reload
		agt.SetReloadFunc(func() error { return mgr.Load() })
		// Set scope router if scopes are configured
		if scopeMgr != nil {
			router := scope.NewRouter(scope.RouterConfig{Type: "local"}, scopeMgr, &poolDispatcher{pool: pool})
			coord.SetScopeRouter(router)
		}
		return coord
	}

	// Determine which config file is active (for startup display)
	configPath := cfgFile
	if configPath == "" {
		projectCfg := filepath.Join(config.ProjectConfigDir, config.ConfigFileName+".yaml")
		if abs, err := filepath.Abs(projectCfg); err == nil {
			projectCfg = abs
		}
		if homeDir, err := os.UserHomeDir(); err == nil {
			candidates := []string{
				projectCfg,
				filepath.Join(homeDir, config.UserConfigDir, config.ConfigFileName+".yaml"),
			}
			for _, p := range candidates {
				if _, err := os.Stat(p); err == nil {
					configPath = p
					break
				}
			}
		}
	}
	printBanner(cfg, configPath)
	return runActorGroup(cfg, mgr, toolRegistry, cdpTool, sessMgr, bus, newCoordinator)
}

func warnNoLLM(cfg *config.Config) {
	defaultModel := cfg.LLM.Model
	if defaultModel == "" {
		defaultModel = "gpt-4o"
	}
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnNoLLM))
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyWarnDefaultModel), defaultModel, cfg.LLM.BaseURL))
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnSetAPIKey))
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnRunSetup))
}

func setupLogging(cfg *config.Config) {
	logger.Init(logger.Config{
		Level:     cfg.Log.Level,
		File:      cfg.Log.File,
		MaxSize:   cfg.Log.MaxSize,
		MaxAge:    cfg.Log.MaxAge,
		MaxBackup: cfg.Log.MaxBackup,
	})
}

// initSkillManager creates and starts the skill manager with multi-directory support.
func initSkillManager(cfg *config.Config) *skill.Manager {
	skillDirs := []string{cfg.Skills.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userSkillsDir := filepath.Join(homeDir, config.UserConfigDir, "skills")
		if userSkillsDir != cfg.Skills.Dir {
			skillDirs = append(skillDirs, userSkillsDir)
		}
	}
	mgr := skill.NewManager(skillDirs...)
	mgr.Load()
	if skills := mgr.List(); len(skills) > 0 {
		zap.S().Infow("skills loaded", "dirs", skillDirs, "count", len(skills))
	}

	// Register built-in skills when self-evolution is enabled
	if cfg.Flags.SelfEvolution {
		if s := appctx.BuiltinSkills; s != "" {
			if err := mgr.Register("self-evolution", "Built-in self-evolution capabilities enabling the agent to create, update, and delete skills and commands", s); err != nil {
				zap.S().Warnw("register builtin skill failed", "error", err)
			} else {
				zap.S().Infow("self-evolution skill registered")
			}
		}
	}

	go mgr.WatchAndReload(context.Background(), 30*time.Second)
	return mgr
}

// initCommandManager creates and loads the user-defined /command manager.
func initCommandManager(cfg *config.Config) *command.Manager {
	cmdDirs := []string{filepath.Join(config.ProjectConfigDir, "commands")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userCmdDir := filepath.Join(homeDir, config.UserConfigDir, "commands")
		cmdDirs = append(cmdDirs, userCmdDir)
	}
	mgr := command.NewManager(cmdDirs...)
	mgr.Load()
	if cmds := mgr.List(); len(cmds) > 0 {
		zap.S().Infow("commands loaded", "dirs", cmdDirs, "count", len(cmds))
	}
	return mgr
}

// initCronManager creates and loads the cron task manager.
func initCronManager(cfg *config.Config) *scheduler.Manager {
	mgr := scheduler.NewManager(cfg.Crontab)
	if err := mgr.Load(); err != nil {
		zap.S().Warnw("crontab load error, continuing without scheduled tasks", "error", err)
	} else {
		zap.S().Infow("crontab loaded", "file", cfg.Crontab.File)
	}
	return mgr
}

// initWorkflowManager creates and starts the workflow manager.
func initWorkflowManager(cfg *config.Config) *workflowpkg.Manager {
	wfDirs := []string{cfg.Workflows.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userWfDir := filepath.Join(homeDir, config.UserConfigDir, "workflows")
		if userWfDir != cfg.Workflows.Dir {
			wfDirs = append(wfDirs, userWfDir)
		}
	}
	mgr := workflowpkg.NewManager(wfDirs...)
	mgr.Load()
	if wfs := mgr.List(); len(wfs) > 0 {
		zap.S().Infow("workflows loaded", "dirs", wfDirs, "count", len(wfs))
	}
	go mgr.WatchAndReload(context.Background(), 30*time.Second)
	return mgr
}

// runActorGroup builds and runs the actor group for all enabled transports and services.

func printBanner(cfg *config.Config, configPath string) {
	providers := cfg.LLM.EffectiveProviders()
	providerName := ""
	providerModel := ""
	if len(providers) > 0 {
		providerName = providers[0].Name
		if providerName == "" {
			providerName = providers[0].Type
		}
		providerModel = providers[0].Model
		if providerModel == "" {
			providerModel = cfg.LLM.Model
		}
	}

	// Collect enabled transport names
	var transports []string
	if cfg.Transport.Stdio.Enabled {
		transports = append(transports, "stdio")
	}
	if cfg.Transport.SSH.Enabled {
		transports = append(transports, "ssh")
	}
	if cfg.Transport.MQTT.Enabled {
		transports = append(transports, "mqtt")
	}
	if cfg.Transport.Email.Enabled {
		transports = append(transports, "email")
	}
	if cfg.Transport.DingTalk.Enabled {
		transports = append(transports, "dingtalk")
	}
	if cfg.Transport.A2A.Enabled {
		transports = append(transports, "a2a")
	}
	transportStr := strings.Join(transports, " ")
	if transportStr == "" {
		transportStr = "none"
	}

	// Check commit hash length for display
	commit := CommitHash
	if len(commit) > 7 {
		commit = commit[:7]
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "==============================================================================")
	fmt.Fprintf(os.Stderr, "  %s\n", i18n.TL(i18n.KeyWelcomeBanner))
	fmt.Fprintf(os.Stderr, "  Version: %s (%s)  %s %s/%s\n", Version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if providerModel != "" {
		fmt.Fprintf(os.Stderr, "  Model:   %s  %s\n", providerModel, providerName)
	}
	fmt.Fprintf(os.Stderr, "  Transport: %s\n", transportStr)
	fmt.Fprintf(os.Stderr, "  Config:  %s\n", configPath)
	fmt.Fprintln(os.Stderr, "==============================================================================")
	fmt.Fprintln(os.Stderr)
}

func runActorGroup(cfg *config.Config, mgr *config.Manager, toolRegistry *mcp.Registry, cdpTool *cdp.Tool, sessMgr *session.Manager, bus *event.EventBus, newCoordinator func() *agent.Coordinator) error {
	var g actor.ActorGroup
	actorCount := 0

	// Signal handling actor (SIGTERM → shutdown, SIGHUP → reload config)
	{
		sigCh := make(chan os.Signal, 1)
		g.Add(actor.Actor{
			Name: "signal",
			Execute: func() error {
				signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
				for sig := range sigCh {
					switch sig {
					case syscall.SIGHUP:
						zap.S().Infow("config reload triggered by SIGHUP")
						if err := mgr.Load(); err != nil {
							zap.S().Errorw("config reload failed on SIGHUP", "error", err)
						} else {
							zap.S().Infow("config reloaded via SIGHUP")
						}
					case syscall.SIGTERM:
						zap.S().Infow("shutting down", "signal", sig)
						return fmt.Errorf("received signal %v", sig)
					}
				}
				return nil
			},
			Interrupt: func(err error) {
				signal.Stop(sigCh)
				close(sigCh)
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				mcpshell.Shutdown()
				if err := telemetry.Shutdown(shutdownCtx); err != nil {
					zap.S().Warnw("telemetry shutdown error", "error", err)
				}
			},
		})
		actorCount++
	}

	// Config file watcher actor
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "config-watch",
			Execute: func() error {
				return mgr.Watch(ctx)
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount++
	}

	// Session reaper actor
	if maxAge, err := time.ParseDuration(cfg.Session.MaxAge); err == nil && maxAge > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "reaper",
			Execute: func() error {
				sessMgr.StartReaper(ctx, maxAge, maxAge/4)
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount++
	} else if err != nil {
		zap.S().Warnw("invalid session.max_age, reaper disabled", "value", cfg.Session.MaxAge, "error", err)
	}

	// Diary actor
	if cfg.Diary.Dir != "" {
		d := diary.New(diary.Config{
			Dir:            cfg.Diary.Dir,
			MaxDaySessions: cfg.Diary.MaxDaySessions,
			MaxWeekDays:    cfg.Diary.MaxWeekDays,
			MaxMonthWeeks:  cfg.Diary.MaxMonthWeeks,
			MaxYearMonths:  cfg.Diary.MaxYearMonths,
			MaxTotalMB:     cfg.Diary.MaxTotalMB,
		}, config.SessionsDir())
		mgr.Subscribe(d)
		go func() { d.Sync() }()

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "diary",
			Execute: func() error {
				for {
					now := time.Now()
					next := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, now.Location())
					if now.After(next) {
						next = next.AddDate(0, 0, 1)
					}
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(next.Sub(now)):
						d.Sync()
					}
				}
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount++
	}

	// Update checker actor
	if cfg.Update.Enabled {
		interval, err := time.ParseDuration(cfg.Update.CheckInterval)
		if err != nil {
			zap.S().Warnw("invalid update.check_interval, update checker disabled", "value", cfg.Update.CheckInterval, "error", err)
		} else {
			ctx, cancel := context.WithCancel(context.Background())
			g.Add(actor.Actor{
				Name: "updater",
				Execute: func() error {
					return update.StartChecker(ctx, update.CheckerConfig{
						Enabled:       cfg.Update.Enabled,
						CheckInterval: interval,
						Channel:       cfg.Update.Channel,
						AutoInstall:   cfg.Update.AutoInstall,
					}, Version)
				},
				Interrupt: func(err error) {
					cancel()
				},
			})
			actorCount++
		}
	}

	// Initialize transports via registry
	factories := transport.Factories()

	// SSH transport
	if cfg.Transport.SSH.Enabled {
		f, ok := factories["ssh"]
		if !ok {
			return fmt.Errorf("ssh transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("ssh transport: %w", err)
		}
		if st, ok := t.(transport.SessionTransport); ok {
			st.SetSessionHandler(func(ctx context.Context, io transport.UserIO) {
				newCoordinator().Run(ctx, io)
			})
			if sub, ok := t.(config.Subscriber); ok {
				mgr.Subscribe(sub)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "ssh",
			Execute: func() error {
				return t.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		actorCount++
	}

	// MQTT broker server (standalone, independent of transport client)
	if cfg.Servers.MQTTBroker.Enabled {
		broker := servermqtt.New(cfg.Servers.MQTTBroker)
		if err := broker.Start(); err != nil {
			return fmt.Errorf("mqtt broker: %w", err)
		}
		defer broker.Close()
		// Wire transport MQTT client to the embedded broker's actual address.
		// This must happen after broker.Start() since ClientAddr() resolves the
		// listening port (important when Addr uses port 0).
		if cfg.Transport.MQTT.Enabled {
			c := mgr.Get()
			if c != nil {
				c.Transport.MQTT.Broker = fmt.Sprintf("tcp://%s", broker.ClientAddr())
			}
		}
	}

	// MQTT transport (client)
	if cfg.Transport.MQTT.Enabled {
		f, ok := factories["mqtt"]
		if !ok {
			return fmt.Errorf("mqtt transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("mqtt transport: %w", err)
		}
		if sub, ok := t.(config.Subscriber); ok {
			mgr.Subscribe(sub)
		}
		uio := t.(transport.UserIO)

		if bp, ok := t.(transport.BannerProvider); ok {
			fmt.Fprint(os.Stderr, bp.Banner())
		}

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "mqtt-server",
			Execute: func() error {
				return t.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		g.Add(actor.Actor{
			Name: "mqtt",
			Execute: func() error {
				go func() {
					newCoordinator().Run(ctx, uio)
					zap.S().Errorw("coordinator exited unexpectedly")
				}()
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount += 2
	}

	// Email transport
	if cfg.Transport.Email.Enabled {
		f, ok := factories["email"]
		if !ok {
			return fmt.Errorf("email transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("email transport: %w", err)
		}
		if sub, ok := t.(config.Subscriber); ok {
			mgr.Subscribe(sub)
		}
		uio := t.(transport.UserIO)

		if bp, ok := t.(transport.BannerProvider); ok {
			fmt.Fprint(os.Stderr, bp.Banner())
		}

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "email-server",
			Execute: func() error {
				return t.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		g.Add(actor.Actor{
			Name: "email",
			Execute: func() error {
				go func() {
					newCoordinator().Run(ctx, uio)
					zap.S().Errorw("coordinator exited unexpectedly")
				}()
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount += 2
	}

	// DingTalk transport
	if cfg.Transport.DingTalk.Enabled {
		f, ok := factories["dingtalk"]
		if !ok {
			return fmt.Errorf("dingtalk transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("dingtalk transport: %w", err)
		}
		if sub, ok := t.(config.Subscriber); ok {
			mgr.Subscribe(sub)
		}
		uio := t.(transport.UserIO)

		if bp, ok := t.(transport.BannerProvider); ok {
			fmt.Fprint(os.Stderr, bp.Banner())
		}

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "dingtalk-server",
			Execute: func() error {
				return t.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		g.Add(actor.Actor{
			Name: "dingtalk",
			Execute: func() error {
				go func() {
					newCoordinator().Run(ctx, uio)
					zap.S().Errorw("coordinator exited unexpectedly")
				}()
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount += 2
	}

	// A2A transport
	if cfg.Transport.A2A.Enabled {
		f, ok := factories["a2a"]
		if !ok {
			return fmt.Errorf("a2a transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("a2a transport: %w", err)
		}
		if sub, ok := t.(config.Subscriber); ok {
			mgr.Subscribe(sub)
		}
		uio := t.(transport.UserIO)

		if bp, ok := t.(transport.BannerProvider); ok {
			fmt.Fprint(os.Stderr, bp.Banner())
		}

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "a2a-server",
			Execute: func() error {
				return t.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		g.Add(actor.Actor{
			Name: "a2a",
			Execute: func() error {
				go func() {
					newCoordinator().Run(ctx, uio)
					zap.S().Errorw("coordinator exited unexpectedly")
				}()
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount += 2
	}

	// Stdio transport
	if cfg.Transport.Stdio.Enabled {
		f, ok := factories["stdio"]
		if !ok {
			return fmt.Errorf("stdio transport not registered")
		}
		t, err := f(cfg)
		if err != nil {
			return fmt.Errorf("stdio transport: %w", err)
		}
		if sub, ok := t.(config.Subscriber); ok {
			mgr.Subscribe(sub)
		}
		uio := t.(transport.UserIO)

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "stdio",
			Execute: func() error {
				newCoordinator().Run(ctx, uio)
				return nil
			},
			Interrupt: func(err error) {
				cancel()
				t.Close()
			},
		})
		actorCount++
	}

	// Pprof HTTP server
	if cfg.Pprof.Enabled {
		srv := &http.Server{
			Addr:              cfg.Pprof.Addr,
			Handler:           http.DefaultServeMux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		host := cfg.Pprof.Addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyPprofBanner), cfg.Pprof.Addr))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyPprofURL), host))
		g.Add(actor.Actor{
			Name: "pprof",
			Execute: func() error {
				if err := srv.ListenAndServe(); err != http.ErrServerClosed {
					return fmt.Errorf("pprof server: %w", err)
				}
				return nil
			},
			Interrupt: func(err error) {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				srv.Shutdown(shutdownCtx)
			},
		})
		actorCount++
	}

	// CDP shutdown actor
	if cdpTool != nil {
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "cdp",
			Execute: func() error {
				<-ctx.Done()
				return nil
			},
			Interrupt: func(err error) {
				cdpTool.Shutdown()
				cancel()
			},
		})
		actorCount++
	}

	// Metrics HTTP server
	if cfg.Metrics.Enabled {
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyMetricsBanner), cfg.Metrics.Addr))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyMetricsURL), cfg.Metrics.Addr))
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		srv := &http.Server{
			Addr:              cfg.Metrics.Addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		g.Add(actor.Actor{
			Name: "metrics",
			Execute: func() error {
				if err := srv.ListenAndServe(); err != http.ErrServerClosed {
					return fmt.Errorf("metrics server: %w", err)
				}
				return nil
			},
			Interrupt: func(err error) {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				srv.Shutdown(shutdownCtx)
			},
		})
		actorCount++
	}

	// Health check HTTP server
	if cfg.Health.Enabled {
		checkers := []health.Checker{
			health.NewChecker("mcp_servers", func(ctx context.Context) error {
				return nil
			}),
			health.NewChecker("plugins", func(ctx context.Context) error {
				return nil
			}),
			health.NewChecker("cron", func(ctx context.Context) error {
				return nil
			}),
		}
		mux := http.NewServeMux()
		mux.Handle("/health", health.Handler(checkers...))
		srv := &http.Server{
			Addr:              cfg.Health.Addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		g.Add(actor.Actor{
			Name: "health",
			Execute: func() error {
				if err := srv.ListenAndServe(); err != http.ErrServerClosed {
					return fmt.Errorf("health server: %w", err)
				}
				return nil
			},
			Interrupt: func(err error) {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				srv.Shutdown(shutdownCtx)
			},
		})
		actorCount++
	}

	// Resource monitor actor
	if cfg.Resource.Enabled {
		rMonitor := resource.New(resource.ConfigFrom(cfg.Resource), bus)
		mgr.Subscribe(rMonitor)
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(actor.Actor{
			Name: "resource-monitor",
			Execute: func() error {
				return rMonitor.Start(ctx)
			},
			Interrupt: func(err error) {
				cancel()
			},
		})
		actorCount++
	}

	if actorCount == 0 {
		return fmt.Errorf("%s", i18n.TL(i18n.KeyTransNoneEnabled))
	}

	return g.Run()
}

// logLevelSubscriber handles runtime log level changes from config reload.
type logLevelSubscriber struct{}

func (logLevelSubscriber) OnConfigChange(oldCfg, newCfg *config.Config) {
	if oldCfg.Log.Level != newCfg.Log.Level {
		logger.SetLevel(newCfg.Log.Level)
	}
}

// poolDispatcher adapts agent.AgentPool to scope.Dispatcher.
type poolDispatcher struct {
	pool *agent.AgentPool
}

func (d *poolDispatcher) Dispatch(agentName string, task scope.DispatchTask) error {
	return d.pool.Dispatch(agentName, agent.Task{
		ID:      task.ID,
		Input:   task.Input,
		Timeout: task.Timeout,
	})
}

func (d *poolDispatcher) PollResult(taskID string) *scope.DispatchResult {
	r := d.pool.PollResult(taskID)
	if r == nil {
		return nil
	}
	return &scope.DispatchResult{
		TaskID:    r.TaskID,
		AgentName: r.AgentName,
		Output:    r.Output,
		Error:     r.Error,
		Success:   r.Success,
	}
}
