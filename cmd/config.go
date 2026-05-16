package cmd

import (
	"fmt"
	"strings"

	"dolphin/internal/config"

	"github.com/spf13/cobra"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show effective configuration",
		RunE:  runConfigShow,
	})

	return cmd
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Println("LLM:")
	if len(cfg.LLM.Providers) > 0 {
		for _, p := range cfg.LLM.Providers {
			fmt.Printf("  - %s: type=%s model=%s url=%s key=%s\n",
				p.Name, p.Type, p.Model, p.BaseURL, maskKey(p.APIKey))
		}
	} else {
		fmt.Printf("  Type:       %s\n", cfg.LLM.Type)
		fmt.Printf("  Model:      %s\n", cfg.LLM.Model)
		fmt.Printf("  Base URL:   %s\n", cfg.LLM.BaseURL)
		fmt.Printf("  API Key:    %s\n", maskKey(cfg.LLM.APIKey))
	}
	fmt.Printf("  Max Tokens: %d\n", cfg.LLM.MaxTokens)
	fmt.Printf("  Max Context Tokens: %d\n", cfg.LLM.MaxContextTokens)
	fmt.Printf("  Temperature: %.1f\n", cfg.LLM.Temperature)
	fmt.Printf("  Max Sub-turns: %d\n", cfg.LLM.MaxSubTurns)
	fmt.Printf("  Compress Mode: %s\n", cfg.LLM.CompressMode)
	fmt.Println()

	fmt.Println("Session:")
	fmt.Printf("  Max Loop: %d\n", cfg.Session.MaxLoop)
	fmt.Printf("  Summary:  %v\n", cfg.Session.Summary)
	fmt.Printf("  Max Age:  %s\n", cfg.Session.MaxAge)
	fmt.Println()

	fmt.Println("Transports:")
	if cfg.Transport.Stdio.Enabled {
		fmt.Println("  stdio:   enabled")
	} else {
		fmt.Println("  stdio:   disabled")
	}
	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		fmt.Printf("  ssh:     enabled (%s, user: %s)\n", addr, cfg.Transport.SSH.Username)
	} else {
		fmt.Println("  ssh:     disabled")
	}
	if cfg.Transport.MQTT.Enabled {
		fmt.Printf("  mqtt:    enabled (broker: %s)\n", cfg.Transport.MQTT.Broker)
	} else {
		fmt.Println("  mqtt:    disabled")
	}
	if cfg.Transport.Email.Enabled {
		fmt.Printf("  email:   enabled (%s)\n", cfg.Transport.Email.From)
	} else {
		fmt.Println("  email:   disabled")
	}
	fmt.Println()

	fmt.Println("MCP Tools:")
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

	fmt.Println("Agent Pool:")
	fmt.Printf("  Max Concurrency:  %d\n", cfg.Pool.MaxConcurrency)
	fmt.Printf("  Default Timeout:  %ds\n", cfg.Pool.DefaultTimeout)
	fmt.Printf("  Idle Timeout:     %ds\n", cfg.Pool.IdleTimeout)
	fmt.Printf("  Workspace:        %s\n", cfg.Pool.WorkspaceDir)
	fmt.Printf("  Max Pending Results: %d\n", cfg.Pool.MaxPendingResults)
	fmt.Println()

	fmt.Println("Skills:")
	fmt.Printf("  Dir:    %s\n", cfg.Skills.Dir)
	fmt.Printf("  Max Top: %d\n", cfg.Skills.MaxTop)
	if len(cfg.Skills.Repos) > 0 {
		fmt.Printf("  Repos:  %v\n", cfg.Skills.Repos)
	}
	fmt.Println()

	fmt.Println("Crontab:")
	fmt.Printf("  File:           %s\n", cfg.Crontab.File)
	fmt.Printf("  Check Interval: %s\n", cfg.Crontab.CheckInterval)
	fmt.Println()

	fmt.Println("Monitoring:")
	fmt.Printf("  Health:  enabled=%v (%s)\n", cfg.Health.Enabled, cfg.Health.Addr)
	fmt.Printf("  Metrics: enabled=%v (%s)\n", cfg.Metrics.Enabled, cfg.Metrics.Addr)
	if cfg.Pprof.Enabled {
		fmt.Printf("  Pprof:   enabled (%s)\n", cfg.Pprof.Addr)
	} else {
		fmt.Println("  Pprof:   disabled")
	}
	fmt.Println()

	fmt.Println("Plugins:")
	fmt.Printf("  Enabled: %v\n", cfg.Plugins.Enabled)
	fmt.Printf("  Dir:     %s\n", cfg.Plugins.Dir)
	fmt.Println()

	fmt.Printf("Log Level: %s\n", cfg.LogLevel)
	fmt.Printf("Log File:  %s\n", cfg.LogFile)

	return nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
