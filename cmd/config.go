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

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowLLM))
	if len(cfg.LLM.Providers) > 0 {
		for _, p := range cfg.LLM.Providers {
			fmt.Fprintf(out, "  - %s: type=%s model=%s url=%s key=%s\n",
				p.Name, p.Type, p.Model, p.BaseURL, maskKey(p.APIKey))
		}
	} else {
		fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowType)+"\n", cfg.LLM.Type)
		fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowModel)+"\n", cfg.LLM.Model)
		fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowBaseURL)+"\n", cfg.LLM.BaseURL)
		fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowAPIKey)+"\n", maskKey(cfg.LLM.APIKey))
	}
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowMaxTokens)+"\n", cfg.LLM.MaxTokens)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowMaxCtxTokens)+"\n", cfg.LLM.MaxContextTokens)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowTemperature)+"\n", cfg.LLM.Temperature)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowMaxSubTurns)+"\n", cfg.LLM.MaxSubTurns)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowCompressMode)+"\n", cfg.LLM.CompressMode)
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowSession))
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowMaxLoop)+"\n", cfg.Session.MaxLoop)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowSummary)+"\n", cfg.Session.Summary)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowMaxAge)+"\n", cfg.Session.MaxAge)
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowTransports))
	if cfg.Transport.Stdio.Enabled {
		fmt.Fprintln(out, "  stdio:   "+i18n.TL(i18n.KeyCfgShowEnabled))
	} else {
		fmt.Fprintln(out, "  stdio:   "+i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		fmt.Fprintf(out, "  ssh:     enabled (%s, user: %s)\n", addr, cfg.Transport.SSH.Username)
	} else {
		fmt.Fprintln(out, "  ssh:     "+i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.MQTT.Enabled {
		fmt.Fprintf(out, "  mqtt:    enabled (broker: %s)\n", cfg.Transport.MQTT.Broker)
	} else {
		fmt.Fprintln(out, "  mqtt:    "+i18n.TL(i18n.KeyCfgShowDisabled))
	}
	if cfg.Transport.Email.Enabled {
		fmt.Fprintf(out, "  email:   enabled (%s)\n", cfg.Transport.Email.From)
	} else {
		fmt.Fprintln(out, "  email:   "+i18n.TL(i18n.KeyCfgShowDisabled))
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowMCP))
	fmt.Fprintf(out, "  Shell:   enabled=%v", cfg.MCP.Shell.Enabled)
	if len(cfg.MCP.Shell.AllowedCommands) > 0 {
		fmt.Fprintf(out, " (restricted: %v)", cfg.MCP.Shell.AllowedCommands)
	} else if cfg.MCP.Shell.AllowUnrestricted {
		fmt.Fprint(out, " (unrestricted)")
	} else {
		fmt.Fprint(out, " (default)")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  CDP:     enabled=%v", cfg.MCP.CDP.Enabled)
	if cfg.MCP.CDP.Enabled {
		if cfg.MCP.CDP.WsURL != "" {
			fmt.Fprintf(out, " (remote: %s)", cfg.MCP.CDP.WsURL)
		} else {
			fmt.Fprintf(out, " (headless: %v)", cfg.MCP.CDP.Headless)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Email:   enabled=%v\n", cfg.MCP.Email.Enabled)
	if len(cfg.MCP.Servers) > 0 {
		for name, s := range cfg.MCP.Servers {
			url := s.URL
			if url == "" {
				url = s.Command + " " + strings.Join(s.Args, " ")
			}
			fmt.Fprintf(out, "  Server(%s): %s (type: %s)\n", name, url, s.Type)
		}
	}
	if len(cfg.MCP.Repos) > 0 {
		fmt.Fprintf(out, "  Repos:   %v\n", cfg.MCP.Repos)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowAgentPool))
	fmt.Fprintf(out, "  Max Concurrency:  %d\n", cfg.Pool.MaxConcurrency)
	fmt.Fprintf(out, "  Default Timeout:  %ds\n", cfg.Pool.DefaultTimeout)
	fmt.Fprintf(out, "  Idle Timeout:     %ds\n", cfg.Pool.IdleTimeout)
	fmt.Fprintf(out, "  Workspace:        %s\n", cfg.Pool.WorkspaceDir)
	fmt.Fprintf(out, "  Max Pending Results: %d\n", cfg.Pool.MaxPendingResults)
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowSkills))
	fmt.Fprintf(out, "  Dir:    %s\n", cfg.Skills.Dir)
	fmt.Fprintf(out, "  Max Top: %d\n", cfg.Skills.MaxTop)
	if len(cfg.Skills.Repos) > 0 {
		fmt.Fprintf(out, "  Repos:  %v\n", cfg.Skills.Repos)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowCrontab))
	fmt.Fprintf(out, "  File:           %s\n", cfg.Crontab.File)
	fmt.Fprintf(out, "  Check Interval: %s\n", cfg.Crontab.CheckInterval)
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowMonitoring))
	fmt.Fprintf(out, "  Health:  enabled=%v (%s)\n", cfg.Health.Enabled, cfg.Health.Addr)
	fmt.Fprintf(out, "  Metrics: enabled=%v (%s)\n", cfg.Metrics.Enabled, cfg.Metrics.Addr)
	if cfg.Pprof.Enabled {
		fmt.Fprintf(out, "  Pprof:   enabled (%s)\n", cfg.Pprof.Addr)
	} else {
		fmt.Fprintln(out, "  Pprof:   disabled")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, i18n.TL(i18n.KeyCfgShowPlugins))
	fmt.Fprintf(out, "  Enabled: %v\n", cfg.Plugins.Enabled)
	fmt.Fprintf(out, "  Dir:     %s\n", cfg.Plugins.Dir)
	fmt.Fprintln(out)

	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowLogLevel)+"\n", cfg.Log.Level)
	fmt.Fprintf(out, i18n.TL(i18n.KeyCfgShowLogFile)+"\n", cfg.Log.File)

	return nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
