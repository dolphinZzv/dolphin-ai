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
	"dolphin/internal/mcp/websearch"
	"dolphin/internal/metrics"
	"dolphin/internal/plugin"
	"dolphin/internal/resource"
	"dolphin/internal/scheduler"
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

	"github.com/oklog/run"
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
	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply --verbose/--quiet log level override
	switch {
	case verbose:
		cfg.LogLevel = "debug"
	case quiet:
		cfg.LogLevel = "error"
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

	// Register built-in tools
	if cfg.MCP.Shell.Enabled {
		toolRegistry.Register(mcpshell.New(cfg))
		zap.S().Infow("shell tool registered")
	}
	var cdpTool *cdp.Tool
	if cfg.MCP.CDP.Enabled {
		cdpTool = cdp.New(cfg)
		toolRegistry.Register(cdpTool)
		zap.S().Infow("cdp tool registered")
	}
	if cfg.MCP.Email.Enabled && cfg.Transport.Email.Username != "" {
		toolRegistry.Register(email.New(cfg))
		zap.S().Infow("email tool registered")
	}
	if cfg.MCP.Webhook.Enabled {
		toolRegistry.Register(webhook.New(cfg))
		zap.S().Infow("webhook tool registered")
	}
	if cfg.MCP.WebSearch.Enabled {
		toolRegistry.Register(websearch.New(cfg))
		zap.S().Infow("web_search tool registered")
	}
	toolRegistry.Register(llm.New(cfg))
	zap.S().Infow("llm tool registered")
	if cfg.MCP.A2A.Enabled {
		toolRegistry.Register(a2a.New(cfg))
		toolRegistry.Register(a2a.NewListTool(cfg))
		zap.S().Infow("a2a tools registered")
	}

	// Load external MCP servers — individual failures are non-fatal.
	if len(cfg.MCP.Servers) > 0 {
		toolRegistry.LoadServers(context.Background())
		defer toolRegistry.CloseServers()
	}

	tools := toolRegistry.List()
	zap.S().Infow("total mcp tools available", "count", len(tools))
	if cfg.LogLevel == "debug" {
		for _, t := range tools {
			zap.S().Debugw("mcp tool", "name", t.Name, "source", t.Source, "desc", t.Description)
		}
	}

	// Check for agents directory to decide coordinator vs single-agent mode
	agentsDir := filepath.Join(".dolphin", "agents")
	_, coordErr := os.Stat(agentsDir)
	hasAgents := coordErr == nil

	poolCfg := agent.NewPoolConfigFromConfig(cfg.Pool)

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
	if err := telemetry.Init(context.Background(), cfg.Telemetry); err != nil {
		zap.S().Warnw("telemetry init failed, continuing without tracing", "error", err)
	} else if cfg.Telemetry.Enabled {
		telemetry.RegisterHooks(hooks)
		if cfg.Telemetry.LogsEnabled {
			telemetry.BridgeZap()
		}
	}

	bus := event.NewEventBus(256)
	pm := plugin.NewManager(hooks, bus)
	toolRegistry.SetEventBus(bus)

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

	// Factory: creates a new coordinator per transport connection
	newCoordinator := func() *agent.Coordinator {
		agt := agent.New(cfg, sessMgr, toolRegistry)
		agt.SetVersion(Version)
		agt.SetBuildTime(BuildTime)
		agt.SetCommitHash(CommitHash)
		agt.SetHooks(hooks)
		agt.SetEventBus(bus)
		agt.SetHeartbeatInterval(cfg.Plugins.HeartbeatTurns)
		pool := agent.NewAgentPool(context.Background(), poolCfg)
		if hasAgents {
			for name, def := range agentDefs {
				pool.Add(name, def, agent.AgentUser, agt, toolRegistry)
			}
		}
		coord := agent.NewCoordinator(agt, pool)
		coord.SetSkillManager(skillMgr)
		coord.SetCommandManager(cmdMgr)
		coord.SetCronManager(cronMgr)
		return coord
	}

	printBanner(cfg)
	return runActorGroup(cfg, toolRegistry, cdpTool, sessMgr, bus, newCoordinator)
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
		Level:     cfg.LogLevel,
		File:      cfg.LogFile,
		MaxSize:   cfg.LogMaxSize,
		MaxAge:    cfg.LogMaxAge,
		MaxBackup: cfg.LogMaxBack,
	})
}

// initSkillManager creates and starts the skill manager with multi-directory support.
func initSkillManager(cfg *config.Config) *skill.Manager {
	skillDirs := []string{cfg.Skills.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userSkillsDir := filepath.Join(homeDir, config.UserConfigDir, "skills")
		if userSkillsDir != cfg.Skills.Dir {
			skillDirs = append([]string{userSkillsDir}, skillDirs...)
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
		cmdDirs = append([]string{userCmdDir}, cmdDirs...)
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
			wfDirs = append([]string{userWfDir}, wfDirs...)
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

func printBanner(cfg *config.Config) {
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

	// Collect enabled transports
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
	fmt.Fprintln(os.Stderr, "==============================================================================")
	fmt.Fprintln(os.Stderr)
}

func runActorGroup(cfg *config.Config, toolRegistry *mcp.Registry, cdpTool *cdp.Tool, sessMgr *session.Manager, bus *event.EventBus, newCoordinator func() *agent.Coordinator) error {
	var g run.Group
	actorCount := 0

	// Signal handling actor
	{
		sigCh := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			sig := <-sigCh
			zap.S().Infow("shutting down", "signal", sig)
			return fmt.Errorf("received signal %v", sig)
		}, func(err error) {
			signal.Stop(sigCh)
			close(sigCh)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := telemetry.Shutdown(shutdownCtx); err != nil {
				zap.S().Warnw("telemetry shutdown error", "error", err)
			}
		})
		actorCount++
	}

	// Session reaper actor
	if maxAge, err := time.ParseDuration(cfg.Session.MaxAge); err == nil && maxAge > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			sessMgr.StartReaper(ctx, maxAge, maxAge/4)
			<-ctx.Done()
			return nil
		}, func(err error) {
			cancel()
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
		go func() { d.Sync() }()

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
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
		}, func(err error) {
			cancel()
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
			g.Add(func() error {
				return update.StartChecker(ctx, update.CheckerConfig{
					Enabled:       cfg.Update.Enabled,
					CheckInterval: interval,
					Channel:       cfg.Update.Channel,
					AutoInstall:   cfg.Update.AutoInstall,
				}, Version)
			}, func(err error) {
				cancel()
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
		}
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyTransSSHServer), addr))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyTransSSHConnect)+"\n", cfg.Transport.SSH.Username, addr[1:]))

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
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
		// Point transport client at the embedded broker if not already set to an external one.
		if cfg.Transport.MQTT.Enabled {
			cfg.Transport.MQTT.Broker = fmt.Sprintf("tcp://%s", broker.ClientAddr())
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
		uio := t.(transport.UserIO)

		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyTransMQTTActive))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyTransMQTTBroker)+"\n\n", cfg.Transport.MQTT.Broker, cfg.Transport.MQTT.SubscribeTopic, cfg.Transport.MQTT.ClientID))

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			go func() {
				newCoordinator().Run(ctx, uio)
				zap.S().Errorw("coordinator exited unexpectedly")
			}()
			<-ctx.Done()
			return nil
		}, func(err error) {
			cancel()
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
		uio := t.(transport.UserIO)

		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyTransEmailActive))
		fmt.Fprintf(os.Stderr, i18n.TL(i18n.KeyTransEmailIMAP)+"\n",
			cfg.Transport.Email.IMAPHost, cfg.Transport.Email.IMAPPort,
			cfg.Transport.Email.PollInterval)
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyTransEmailSMTP)+"\n", cfg.Transport.Email.SMTPHost, cfg.Transport.Email.SMTPPort))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyTransEmailHint)+"\n\n", cfg.Transport.Email.From))

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			go func() {
				newCoordinator().Run(ctx, uio)
				zap.S().Errorw("coordinator exited unexpectedly")
			}()
			<-ctx.Done()
			return nil
		}, func(err error) {
			cancel()
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
		uio := t.(transport.UserIO)

		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyTransDingTalk))

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			go func() {
				newCoordinator().Run(ctx, uio)
				zap.S().Errorw("coordinator exited unexpectedly")
			}()
			<-ctx.Done()
			return nil
		}, func(err error) {
			cancel()
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
		uio := t.(transport.UserIO)

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			go func() {
				newCoordinator().Run(ctx, uio)
				zap.S().Errorw("coordinator exited unexpectedly")
			}()
			<-ctx.Done()
			return nil
		}, func(err error) {
			cancel()
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
		uio := t.(transport.UserIO)

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			newCoordinator().Run(ctx, uio)
			return nil
		}, func(err error) {
			cancel()
			t.Close()
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
		g.Add(func() error {
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return fmt.Errorf("pprof server: %w", err)
			}
			return nil
		}, func(err error) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
		})
		actorCount++
	}

	// CDP shutdown actor
	if cdpTool != nil {
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			<-ctx.Done()
			return nil
		}, func(err error) {
			cdpTool.Shutdown()
			cancel()
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
		g.Add(func() error {
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return fmt.Errorf("metrics server: %w", err)
			}
			return nil
		}, func(err error) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
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
		g.Add(func() error {
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return fmt.Errorf("health server: %w", err)
			}
			return nil
		}, func(err error) {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			srv.Shutdown(shutdownCtx)
		})
		actorCount++
	}

	// Resource monitor actor
	if cfg.Resource.Enabled {
		rMonitor := resource.New(resource.ConfigFrom(cfg.Resource), bus)
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return rMonitor.Start(ctx)
		}, func(err error) {
			cancel()
		})
		actorCount++
	}

	if actorCount == 0 {
		return fmt.Errorf("%s", i18n.TL(i18n.KeyTransNoneEnabled))
	}

	return g.Run()
}
