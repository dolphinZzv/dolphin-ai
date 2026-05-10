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

	"dolphinzZ/internal/agent"
	"dolphinzZ/internal/command"
	"dolphinzZ/internal/config"
	"dolphinzZ/internal/i18n"
	"dolphinzZ/internal/logger"
	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/metrics"
	"dolphinzZ/internal/scheduler"
	"dolphinzZ/internal/session"
	"dolphinzZ/internal/skill"
	"dolphinzZ/internal/transport"

	"github.com/oklog/run"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	_ "net/http/pprof"
)

var (
	cfgFile string
	Version = "dev"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dolphinzZ",
		Short: "AI Agent — stdio / SSH / MQTT / Email transport, MCP tools (shell + cdp)",
		Long: `DolphinzZ is an AI Agent with MCP tool support.

Transports: stdio (default), SSH (:2222), MQTT, Email
Tools: shell, cdp (browser automation)
Config: .dolphinzZ/config.yaml > ~/.dolphinzZ/ > /etc/dolphinzZ/
Env: DZ_LLM_API_KEY, DZ_LLM_MODEL, DZ_LLM_BASE_URL`,
		RunE:    runAgent,
		Version: Version,
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "path to config file (searches .dolphinzZ/, ~/.dolphinzZ/, /etc/dolphinzZ/ by default)")
	cmd.SetVersionTemplate("dolphinzZ {{.Version}}\n")

	cmd.AddCommand(NewSetupCmd())

	return cmd
}

func runAgent(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Setup logging
	setupLogging(cfg)
	zap.S().Infow("config loaded", "session_dir", cfg.Session.Dir)

	// First-run career-guided tool loading (only when stdio is the transport)
	if config.IsFirstRun() && cfg.Transport.Stdio.Enabled {
		profile, err := config.RunFirstRunPrompt()
		if err != nil {
			zap.S().Warnw("first-run prompt failed", "error", err)
		}
		if profile != nil {
			fmt.Fprintf(os.Stderr, "\n=== %s: %s ===\n", i18n.TL(i18n.KeyRecommendedTools), profile.Description)
			// Augment built-in mapping with tools from configured repos (best-effort)
			extraSkills, extraMCP := config.AugmentWithRepos(profile, cfg.Skills.Repos, cfg.MCP.Repos)
			allSkills := append([]string{}, profile.Skills...)
			allSkills = append(allSkills, extraSkills...)
			allMCP := append([]string{}, profile.MCP...)
			allMCP = append(allMCP, extraMCP...)

			if len(allSkills) > 0 {
				fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.TL(i18n.KeySkills), strings.Join(allSkills, ", "))
			}
			if len(allMCP) > 0 {
				fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.TL(i18n.KeyMCP), strings.Join(allMCP, ", "))
			}
			fmt.Fprintf(os.Stderr, "\n%s\n", i18n.TL(i18n.KeyInstallHint))
			fmt.Fprintf(os.Stderr, "%s\n\n", i18n.TL(i18n.KeySetupHint))

			// Ask about SYSTEM.md generation (first run only)
			config.PromptSystemMD()
		}
		// Always create the marker so first-run only triggers once
		config.CreateFirstRunMarker()
	}

	// Init session manager
	sessMgr := session.NewManager(cfg.Session.Dir)
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

	// Load external MCP servers
	if len(cfg.MCP.Servers) > 0 {
		if err := toolRegistry.LoadServers(); err != nil {
			return fmt.Errorf("load mcp servers: %w", err)
		}
		defer toolRegistry.CloseServers()
		zap.S().Infow("external mcp servers loaded", "count", len(cfg.MCP.Servers))
	}

	tools := toolRegistry.List()
	zap.S().Infow("total mcp tools available", "count", len(tools))

	// Check for agents directory to decide coordinator vs single-agent mode
	agentsDir := filepath.Join(".dolphinzZ", "agents")
	_, coordErr := os.Stat(agentsDir)
	hasAgents := coordErr == nil

	poolCfg := agent.NewPoolConfigFromConfig(cfg.Pool)

	// Pre-load user-created agent definitions (shared across connections)
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

	// Initialize skill manager with multi-directory support (user + project)
	skillDirs := []string{cfg.Skills.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userSkillsDir := filepath.Join(homeDir, config.UserConfigDir, "skills")
		if userSkillsDir != cfg.Skills.Dir {
			skillDirs = append([]string{userSkillsDir}, skillDirs...)
		}
	}
	skillMgr := skill.NewManager(skillDirs...)
	skillMgr.Load()
	if skills := skillMgr.List(); len(skills) > 0 {
		zap.S().Infow("skills loaded", "dirs", skillDirs, "count", len(skills))
	}
	// Start skill hot-reload watcher (ticker-based polling)
	go skillMgr.WatchAndReload(context.Background(), 30*time.Second)

	// Initialize user-defined /command manager (multi-dir: user + project)
	cmdDirs := []string{filepath.Join(config.ProjectConfigDir, "commands")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userCmdDir := filepath.Join(homeDir, config.UserConfigDir, "commands")
		cmdDirs = append([]string{userCmdDir}, cmdDirs...)
	}
	cmdMgr := command.NewManager(cmdDirs...)
	cmdMgr.Load()
	if cmds := cmdMgr.List(); len(cmds) > 0 {
		zap.S().Infow("commands loaded", "dirs", cmdDirs, "count", len(cmds))
	}

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
	cronMgr := scheduler.NewManager(cfg.Crontab)
	if err := cronMgr.Load(); err != nil {
		zap.S().Warnw("crontab load error, continuing without scheduled tasks", "error", err)
	} else {
		zap.S().Infow("crontab loaded", "file", cfg.Crontab.File)
	}

	// Factory: creates a new coordinator per transport connection
	newCoordinator := func() *agent.Coordinator {
		agt := agent.New(cfg, sessMgr, toolRegistry)
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
		// Non-blocking: pick up async recommendation if ready
		select {
		case rec := <-recommendCh:
			if rec != nil {
				coord.SetStartupRecommend(rec)
			}
		default:
		}
		return coord
	}

	// ---- Actor group ----

	var g run.Group
	actorCount := 0

	// Signal handling actor — exits the group on SIGINT/SIGTERM
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

	// Session reaper actor — periodically cleans old session files
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
		fmt.Fprintf(os.Stderr, "\n=== SSH server listening on %s ===\n", addr)
		fmt.Fprintf(os.Stderr, "Connect: ssh %s@<host> -p %s\n", cfg.Transport.SSH.Username, addr[1:])
		zap.S().Infow("ssh transport listening", "addr", addr)

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return t.Start(ctx)
		}, func(err error) {
			cancel()
			t.Close()
		})
		actorCount++
	}

	// MQTT transport + coordinator
	if cfg.Transport.MQTT.Enabled {
		zap.S().Infow("starting mqtt transport", "broker", cfg.Transport.MQTT.Broker)
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

	// Email transport + coordinator
	if cfg.Transport.Email.Enabled {
		ctx, cancel := context.WithCancel(context.Background())
		t := transport.NewEmailTransport(&cfg.Transport.Email)

		fmt.Fprintf(os.Stderr, "\n=== Email transport active ===\n")
		fmt.Fprintf(os.Stderr, "IMAP: %s:%d (poll every %s)\n",
			cfg.Transport.Email.IMAPHost, cfg.Transport.Email.IMAPPort,
			cfg.Transport.Email.PollInterval)
		fmt.Fprintf(os.Stderr, "SMTP: %s:%d\n", cfg.Transport.Email.SMTPHost, cfg.Transport.Email.SMTPPort)
		fmt.Fprintf(os.Stderr, "Send an email to %s — subject = command\n\n", cfg.Transport.Email.From)

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

	// Stdio transport + coordinator
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
		g.Add(func() error {
			zap.S().Infow("pprof HTTP server starting", "addr", cfg.Pprof.Addr)
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return fmt.Errorf("pprof server: %w", err)
			}
			return nil
		}, func(err error) {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer shutdownCancel()
			srv.Shutdown(shutdownCtx)
		})
		actorCount++
	}

	// CDP shutdown actor — ensures Chrome is killed on process exit
	if cdpTool != nil {
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			<-ctx.Done()
			return nil
		}, func(err error) {
			zap.S().Debugw("cdp: shutting down browser on exit")
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
			zap.S().Infow("metrics HTTP server starting", "addr", cfg.Metrics.Addr)
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				return fmt.Errorf("metrics server: %w", err)
			}
			return nil
		}, func(err error) {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer shutdownCancel()
			srv.Shutdown(shutdownCtx)
		})
		actorCount++
	}

	if actorCount == 0 {
		return fmt.Errorf("no transport enabled (enable stdio, ssh, mqtt, or email in config)")
	}

	return g.Run()
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
