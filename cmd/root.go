package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"dolphinzZ/internal/agent"
	"dolphinzZ/internal/config"
	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/session"
	"dolphinzZ/internal/skill"
	"dolphinzZ/internal/transport"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	Version = "dev"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dolphinzZ",
		Short: "AI coding agent — stdio / SSH / MQTT transport, MCP tools (shell + cdp)",
		Long: `DolphinzZ is an AI coding agent with MCP tool support.

Transports: stdio (default), SSH (:2222), MQTT
Tools: shell, cdp (browser automation)
Config: .dolphinzZ/config.yaml > ~/.dolphinzZ/ > /etc/dolphinzZ/
Env: DZ_LLM_API_KEY, DZ_LLM_MODEL, DZ_LLM_BASE_URL`,
		RunE:    runAgent,
		Version: Version,
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "path to config file (searches .dolphinzZ/, ~/.dolphinzZ/, /etc/dolphinzZ/ by default)")
	cmd.SetVersionTemplate("dolphinzZ {{.Version}}\n")

	return cmd
}

func runAgent(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Setup logging
	setupLogging(cfg.LogLevel)
	slog.Info("config loaded", "session_dir", cfg.Session.Dir)

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
		slog.Info("shell tool registered")
	}
	if cfg.MCP.CDP.Enabled {
		toolRegistry.Register(mcp.NewCDPTool(cfg))
		slog.Info("cdp tool registered")
	}

	// Load external MCP servers
	if len(cfg.MCP.Servers) > 0 {
		if err := toolRegistry.LoadServers(); err != nil {
			return fmt.Errorf("load mcp servers: %w", err)
		}
		defer toolRegistry.CloseServers()
		slog.Info("external mcp servers loaded", "count", len(cfg.MCP.Servers))
	}

	tools := toolRegistry.List()
	slog.Info("total mcp tools available", "count", len(tools))

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start session reaper if max_age is configured
	if maxAge, err := time.ParseDuration(cfg.Session.MaxAge); err == nil && maxAge > 0 {
		sessMgr.StartReaper(ctx, maxAge, maxAge/4)
	} else if err != nil {
		slog.Warn("invalid session.max_age, reaper disabled", "value", cfg.Session.MaxAge, "error", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down...")
		cancel()
	}()

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
		slog.Info("coordinator mode enabled", "agents_dir", agentsDir, "count", len(agentDefs))
	} else {
		slog.Info("no agents directory, using single-agent mode", "dir", agentsDir)
	}

	// Initialize skill manager
	skillMgr := skill.NewManager(cfg.Skills.Dir)
	if err := skillMgr.Load(); err != nil {
		slog.Debug("skills directory not found, skills disabled", "dir", cfg.Skills.Dir, "error", err)
	} else {
		skills := skillMgr.List()
		slog.Info("skills loaded", "dir", cfg.Skills.Dir, "count", len(skills))
	}

	// Factory: creates a new coordinator per transport connection
	newCoordinator := func() *agent.Coordinator {
		agt := agent.New(cfg, sessMgr, toolRegistry)
		pool := agent.NewAgentPool(ctx, poolCfg)
		if hasAgents {
			for name, def := range agentDefs {
				pool.Add(name, def, agent.AgentUser, agt, toolRegistry)
			}
		}
		coord := agent.NewCoordinator(agt, pool)
			coord.SetSkillManager(skillMgr)
			return coord
	}

	// Start transports
	started := false

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
		fmt.Fprintf(os.Stderr, "Password: %s\n\n", cfg.Transport.SSH.Password)
		slog.Info("ssh transport listening", "addr", addr)
		go func() {
			if err := t.Start(ctx); err != nil {
				slog.Error("ssh server error", "error", err)
			}
		}()
		started = true
	}

	if cfg.Transport.Stdio.Enabled {
		slog.Info("starting stdio transport")
		io := transport.NewStdioTransport()
		newCoordinator().Run(ctx, io)
		started = true
	}

	if cfg.Transport.MQTT.Enabled {
		slog.Info("starting mqtt transport", "broker", cfg.Transport.MQTT.Broker)
		t := transport.NewMQTTTransport(cfg)
		go func() {
			if err := t.Start(ctx); err != nil {
				slog.Error("mqtt transport error", "error", err)
			}
		}()
		time.Sleep(500 * time.Millisecond)
		go func() {
			newCoordinator().Run(ctx, t)
		}()
		started = true
	}

	if !started {
		return fmt.Errorf("no transport enabled (enable stdio, ssh, or mqtt in config)")
	}

	return nil
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
}
