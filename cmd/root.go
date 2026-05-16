package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	"dolphin/internal/i18n"
	"dolphin/internal/logger"
	"dolphin/internal/mcp"
	"dolphin/internal/metrics"
	"dolphin/internal/plugin"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/transport"
	"dolphin/internal/update"

	"github.com/oklog/run"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	_ "net/http/pprof"
)

var (
	cfgFile   string
	verbose   bool
	quiet     bool
	Version   = "dev"
	BuildTime = "unknown"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dolphin",
		Short: "AI Agent — stdio / SSH / MQTT / Email transport, MCP tools (shell + cdp)",
		Long: `dolphin is an AI Agent with MCP tool support.

Transports: stdio (default), SSH (:2222), MQTT, Email
Tools: shell, cdp (browser automation)
Config: .dolphin/config.yaml > ~/.dolphin/ > /etc/dolphin/
Env: DZ_LLM_API_KEY, DZ_LLM_MODEL, DZ_LLM_BASE_URL`,
		RunE:    runAgent,
		Version: Version,
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "path to config file (searches .dolphin/, ~/.dolphin/, /etc/dolphin/ by default)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug-level logging")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-error output")
	cmd.SetVersionTemplate("dolphin {{.Version}}\n")

	cmd.AddCommand(NewSetupCmd())
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

	// First-run career-guided tool loading
	firstRunSetup(cfg)

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
		toolRegistry.Register(mcp.NewShellTool(cfg))
		zap.S().Infow("shell tool registered")
	}
	var cdpTool *mcp.CDPTool
	if cfg.MCP.CDP.Enabled {
		cdpTool = mcp.NewCDPTool(cfg)
		toolRegistry.Register(cdpTool)
		zap.S().Infow("cdp tool registered")
	}
	if cfg.MCP.Email.Enabled && cfg.Transport.Email.Username != "" {
		toolRegistry.Register(mcp.NewEmailTool(cfg))
		zap.S().Infow("email tool registered")
	}
	if cfg.MCP.Webhook.Enabled {
		toolRegistry.Register(mcp.NewWebhookTool(cfg))
		zap.S().Infow("webhook tool registered")
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

	// Launch async project detection + repo recommendation (non-blocking)
	recommendCh := make(chan *config.Recommendation, 1)
	go func() {
		workDir, _ := os.Getwd()
		rec := config.RecommendTools(context.Background(), workDir, nil, cfg.Skills.Repos, cfg.MCP.Repos)
		if rec != nil && (len(rec.Skills) > 0 || len(rec.MCP) > 0) {
			recommendCh <- rec
		}
		close(recommendCh)
	}()

	// Initialize cron task manager
	cronMgr := initCronManager(cfg)

	// Init plugin system: hooks (sync) + events (async)
	hooks := hook.NewRegistry()
	bus := event.NewEventBus(256)
	pm := plugin.NewManager(hooks, bus)

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
		select {
		case rec := <-recommendCh:
			if rec != nil {
				coord.SetStartupRecommend(rec)
			}
		default:
		}
		return coord
	}

	return runActorGroup(cfg, toolRegistry, cdpTool, sessMgr, newCoordinator)
}

// isDevMode returns true when DZ_DEV=true (integration test mode).
func isDevMode() bool {
	return os.Getenv("DZ_DEV") == "true"
}

// firstRunSetup runs the first-run career-guided tool loading wizard.
// In dev mode (DZ_DEV=true), skips the interactive prompt and auto-loads a demo career.
func firstRunSetup(cfg *config.Config) {
	if !config.IsFirstRun() || !cfg.Transport.Stdio.Enabled {
		return
	}

	var profile *config.CareerProfile
	var err error

	if isDevMode() {
		profile = &config.CareerProfile{
			Name:        "demo",
			Skills:      []string{"demo-skill"},
			MCP:         []string{"filesystem"},
			Description: "Demo (integration test)",
		}
		cfg.Skills.Repos = append([]string{"dolphinZzv/demo_skills"}, cfg.Skills.Repos...)
		cfg.MCP.Repos = append([]string{"dolphinv/mcp"}, cfg.MCP.Repos...)
		fmt.Fprintf(os.Stderr, "\n[dev] Auto-loading demo career profile\n")
	} else {
		profile, err = config.RunFirstRunPrompt()
		if err != nil {
			zap.S().Warnw("first-run prompt failed", "error", err)
			return
		}
	}
	if profile == nil {
		config.CreateFirstRunMarker()
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== %s: %s ===\n", i18n.TL(i18n.KeyRecommendedTools), profile.Description)
	extraSkills, extraMCP := config.AugmentWithRepos(profile, cfg.Skills.Repos, cfg.MCP.Repos)

	// Apply matched tools: download skills, add MCP servers to config
	if err := config.ApplyTools(extraSkills, extraMCP); err != nil {
		zap.S().Warnw("apply tools failed", "error", err)
	}
	// Merge into in-memory config so MCP servers take effect immediately
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = make(map[string]config.MCPServerConfig)
	}
	for _, m := range extraMCP {
		if m.Command == "" {
			continue
		}
		if _, exists := cfg.MCP.Servers[m.Name]; exists {
			continue
		}
		cfg.MCP.Servers[m.Name] = config.MCPServerConfig{
			Type:    "stdio",
			Command: m.Command,
			Args:    m.Args,
		}
	}

	// Display deduplicated matched tools
	seenSkills := make(map[string]bool)
	var skillNames []string
	for _, s := range profile.Skills {
		if !seenSkills[s] {
			seenSkills[s] = true
			skillNames = append(skillNames, s)
		}
	}
	for _, s := range extraSkills {
		if !seenSkills[s.Name] {
			seenSkills[s.Name] = true
			skillNames = append(skillNames, s.Name)
		}
	}
	seenMCP := make(map[string]bool)
	var mcpNames []string
	for _, m := range profile.MCP {
		if !seenMCP[m] {
			seenMCP[m] = true
			mcpNames = append(mcpNames, m)
		}
	}
	for _, m := range extraMCP {
		if !seenMCP[m.Name] {
			seenMCP[m.Name] = true
			mcpNames = append(mcpNames, m.Name)
		}
	}

	if len(skillNames) > 0 {
		fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.TL(i18n.KeySkills), strings.Join(skillNames, ", "))
	}
	if len(mcpNames) > 0 {
		fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.TL(i18n.KeyMCP), strings.Join(mcpNames, ", "))
	}
	if len(skillNames) > 0 || len(mcpNames) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n", i18n.TL(i18n.KeyToolsInstalled))
	}

	config.PromptSystemMD()
	config.PromptConfigFile()
	config.CreateFirstRunMarker()
}

func warnNoLLM(cfg *config.Config) {
	defaultModel := cfg.LLM.Model
	if defaultModel == "" {
		defaultModel = "gpt-4o"
	}
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnNoLLM))
	fmt.Fprintf(os.Stderr, i18n.TL(i18n.KeyWarnDefaultModel), defaultModel, cfg.LLM.BaseURL)
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnSetAPIKey))
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyWarnRunSetup))
}

func setupLogging(cfg *config.Config) {
	logger.Init(logger.Config{
		Level:     cfg.LogLevel,
		File:      cfg.LogFile,
		MaxSize:   100,
		MaxAge:    30,
		MaxBackup: 3,
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

// runActorGroup builds and runs the actor group for all enabled transports and services.
func runActorGroup(cfg *config.Config, toolRegistry *mcp.Registry, cdpTool *mcp.CDPTool, sessMgr *session.Manager, newCoordinator func() *agent.Coordinator) error {
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

	// SSH transport
	if cfg.Transport.SSH.Enabled {
		t, err := transport.NewSSHTransport(cfg, func(ctx context.Context, io transport.UserIO) {
			newCoordinator().Run(ctx, io)
		})
		if err != nil {
			return fmt.Errorf("ssh transport: %w", err)
		}
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		fmt.Fprintf(os.Stderr, "\n=== SSH server configured on %s ===\n", addr)
		fmt.Fprintf(os.Stderr, "Connect: ssh %s@<host> -p %s\n", cfg.Transport.SSH.Username, addr[1:])

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		actorCount++
	}

	// MQTT transport
	if cfg.Transport.MQTT.Enabled {
		// Start embedded MQTT broker when configured, so no external broker is needed.
		if cfg.Transport.MQTT.Embedded {
			accounts := cfg.Transport.MQTT.EmbeddedAccounts
			broker := transport.NewEmbeddedBroker(cfg.Transport.MQTT.EmbeddedAddr, accounts)
			if err := broker.Start(accounts); err != nil {
				return fmt.Errorf("embedded mqtt broker: %w", err)
			}
			defer broker.Close()
			cfg.Transport.MQTT.Broker = fmt.Sprintf("tcp://%s", broker.ClientAddr())
			// Auto-populate client credentials from the first embedded account.
			if cfg.Transport.MQTT.Username == "" && len(accounts) > 0 {
				cfg.Transport.MQTT.Username = accounts[0].Username
				cfg.Transport.MQTT.Password = accounts[0].Password
			}
		}

		fmt.Fprintf(os.Stderr, "\n=== MQTT transport active ===\n")
		fmt.Fprintf(os.Stderr, "Broker: %s  Topic: %s  Client: %s\n\n",
			cfg.Transport.MQTT.Broker, cfg.Transport.MQTT.Topic, cfg.Transport.MQTT.ClientID)

		ctx, cancel := context.WithCancel(context.Background())
		t := transport.NewMQTTTransport(cfg)

		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			newCoordinator().Run(ctx, t)
			return nil
		}, func(err error) {
			cancel()
		})
		actorCount += 2
	}

	// Email transport
	if cfg.Transport.Email.Enabled {
		fmt.Fprintf(os.Stderr, "\n=== Email transport active ===\n")
		fmt.Fprintf(os.Stderr, "IMAP: %s:%d (poll every %s)\n",
			cfg.Transport.Email.IMAPHost, cfg.Transport.Email.IMAPPort,
			cfg.Transport.Email.PollInterval)
		fmt.Fprintf(os.Stderr, "SMTP: %s:%d\n", cfg.Transport.Email.SMTPHost, cfg.Transport.Email.SMTPPort)
		fmt.Fprintf(os.Stderr, "Send an email to %s — subject = command\n\n", cfg.Transport.Email.From)

		ctx, cancel := context.WithCancel(context.Background())
		t := transport.NewEmailTransport(&cfg.Transport.Email)

		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			newCoordinator().Run(ctx, t)
			return nil
		}, func(err error) {
			cancel()
		})
		actorCount += 2
	}

	// DingTalk transport
	if cfg.Transport.DingTalk.Enabled {
		fmt.Fprintf(os.Stderr, "\n=== DingTalk bot active (Stream mode) ===\n")

		ctx, cancel := context.WithCancel(context.Background())
		t := transport.NewDingTalkTransport(&cfg.Transport.DingTalk)

		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		g.Add(func() error {
			newCoordinator().Run(ctx, t)
			return nil
		}, func(err error) {
			cancel()
		})
		actorCount += 2
	}

	// Stdio transport
	if cfg.Transport.Stdio.Enabled {
		ctx, cancel := context.WithCancel(context.Background())
		io := transport.NewStdioTransport()

		g.Add(func() error {
			newCoordinator().Run(ctx, io)
			return nil
		}, func(err error) {
			cancel()
			io.Close()
		})
		actorCount++
	}

	// Pprof HTTP server
	if cfg.Pprof.Enabled {
		srv := &http.Server{
			Addr:    cfg.Pprof.Addr,
			Handler: http.DefaultServeMux,
		}
		host := cfg.Pprof.Addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		fmt.Fprintf(os.Stderr, i18n.TL(i18n.KeyPprofBanner), cfg.Pprof.Addr)
		fmt.Fprintf(os.Stderr, i18n.TL(i18n.KeyPprofURL), host)
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
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		srv := &http.Server{
			Addr:    cfg.Metrics.Addr,
			Handler: mux,
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
			Addr:    cfg.Health.Addr,
			Handler: mux,
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

	if actorCount == 0 {
		return fmt.Errorf("no transport enabled (enable stdio, ssh, mqtt, or email in config)")
	}

	return g.Run()
}
