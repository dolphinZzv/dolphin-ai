package cmd

import (
	"fmt"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdConfigUse),
		Short: i18n.TL(i18n.KeyCmdConfigShort),
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdConfigShowUse),
		Short: i18n.TL(i18n.KeyCmdConfigShowShort),
		RunE:  runConfigShow,
	})

	return cmd
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Println(i18n.TL(i18n.KeyCfgShowLLM))
	if len(cfg.LLM.Providers) > 0 {
		for _, p := range cfg.LLM.Providers {
			fmt.Printf("  - %s: type=%s model=%s url=%s key=%s\n",
				p.Name, p.Type, p.Model, p.BaseURL, maskKey(p.APIKey))
		}
	} else {
		fmt.Printf(i18n.TL(i18n.KeyCfgShowType)+"\n", cfg.LLM.Type)
		fmt.Printf(i18n.TL(i18n.KeyCfgShowModel)+"\n", cfg.LLM.Model)
		fmt.Printf(i18n.TL(i18n.KeyCfgShowBaseURL)+"\n", cfg.LLM.BaseURL)
		fmt.Printf(i18n.TL(i18n.KeyCfgShowAPIKey)+"\n", maskKey(cfg.LLM.APIKey))
	}
	fmt.Printf(i18n.TL(i18n.KeyCfgShowMaxTokens)+"\n", cfg.LLM.MaxTokens)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowMaxCtxTokens)+"\n", cfg.LLM.MaxContextTokens)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowTemperature)+"\n", cfg.LLM.Temperature)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowMaxSubTurns)+"\n", cfg.LLM.MaxSubTurns)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowCompressMode)+"\n", cfg.LLM.CompressMode)
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowSession))
	fmt.Printf(i18n.TL(i18n.KeyCfgShowMaxLoop)+"\n", cfg.Session.MaxLoop)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowSummary)+"\n", cfg.Session.Summary)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowMaxAge)+"\n", cfg.Session.MaxAge)
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowTransports))
	if cfg.Transport.Stdio.Enabled {
		fmt.Println("  stdio:   " + i18n.TL(i18n.KeyCfgShowEnabled))
	} else {
		fmt.Println("  stdio:   " + i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		fmt.Printf("  ssh:     enabled (%s, user: %s)\n", addr, cfg.Transport.SSH.Username)
	} else {
		fmt.Println("  ssh:     " + i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.MQTT.Enabled {
		fmt.Printf("  mqtt:    enabled (broker: %s)\n", cfg.Transport.MQTT.Broker)
	} else {
		fmt.Println("  mqtt:    " + i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.Email.Enabled {
		fmt.Printf("  email:   enabled (%s)\n", cfg.Transport.Email.From)
	} else {
		fmt.Println("  email:   " + i18n.TL(i18n.KeyCfgShowDisabled))
	}
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowMCP))
	fmt.Printf("  Shell:   enabled=%v", cfg.MCP.Shell.Enabled)
	if len(cfg.MCP.Shell.AllowedCommands) > 0 {
		fmt.Printf(" (restricted: %v)", cfg.MCP.Shell.AllowedCommands)
	} else if cfg.MCP.Shell.AllowUnrestricted {
		fmt.Print(" (unrestricted)")
	} else {
		fmt.Print(" (default)")
	}
	fmt.Println()
	fmt.Printf("  CDP:     enabled=%v", cfg.MCP.CDP.Enabled)
	if cfg.MCP.CDP.Enabled {
		if cfg.MCP.CDP.WsURL != "" {
			fmt.Printf(" (remote: %s)", cfg.MCP.CDP.WsURL)
		} else {
			fmt.Printf(" (headless: %v)", cfg.MCP.CDP.Headless)
		}
	}
	fmt.Println()
	fmt.Printf("  Email:   enabled=%v\n", cfg.MCP.Email.Enabled)
	if len(cfg.MCP.Servers) > 0 {
		for name, s := range cfg.MCP.Servers {
			url := s.URL
			if url == "" {
				url = s.Command + " " + strings.Join(s.Args, " ")
			}
			fmt.Printf("  Server(%s): %s (type: %s)\n", name, url, s.Type)
		}
	}
	if len(cfg.MCP.Repos) > 0 {
		fmt.Printf("  Repos:   %v\n", cfg.MCP.Repos)
	}
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowAgentPool))
	fmt.Printf("  Max Concurrency:  %d\n", cfg.Pool.MaxConcurrency)
	fmt.Printf("  Default Timeout:  %ds\n", cfg.Pool.DefaultTimeout)
	fmt.Printf("  Idle Timeout:     %ds\n", cfg.Pool.IdleTimeout)
	fmt.Printf("  Workspace:        %s\n", cfg.Pool.WorkspaceDir)
	fmt.Printf("  Max Pending Results: %d\n", cfg.Pool.MaxPendingResults)
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowSkills))
	fmt.Printf("  Dir:    %s\n", cfg.Skills.Dir)
	fmt.Printf("  Max Top: %d\n", cfg.Skills.MaxTop)
	if len(cfg.Skills.Repos) > 0 {
		fmt.Printf("  Repos:  %v\n", cfg.Skills.Repos)
	}
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowCrontab))
	fmt.Printf("  File:           %s\n", cfg.Crontab.File)
	fmt.Printf("  Check Interval: %s\n", cfg.Crontab.CheckInterval)
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowMonitoring))
	fmt.Printf("  Health:  enabled=%v (%s)\n", cfg.Health.Enabled, cfg.Health.Addr)
	fmt.Printf("  Metrics: enabled=%v (%s)\n", cfg.Metrics.Enabled, cfg.Metrics.Addr)
	if cfg.Pprof.Enabled {
		fmt.Printf("  Pprof:   enabled (%s)\n", cfg.Pprof.Addr)
	} else {
		fmt.Println("  Pprof:   disabled")
	}
	fmt.Println()

	fmt.Println(i18n.TL(i18n.KeyCfgShowPlugins))
	fmt.Printf("  Enabled: %v\n", cfg.Plugins.Enabled)
	fmt.Printf("  Dir:     %s\n", cfg.Plugins.Dir)
	fmt.Println()

	fmt.Printf(i18n.TL(i18n.KeyCfgShowLogLevel)+"\n", cfg.LogLevel)
	fmt.Printf(i18n.TL(i18n.KeyCfgShowLogFile)+"\n", cfg.LogFile)

	return nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
