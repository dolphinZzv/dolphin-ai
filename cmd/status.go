package cmd

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdStatusUse),
		Short: i18n.TL(i18n.KeyCmdStatusShort),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			fmt.Printf(i18n.TL(i18n.KeyStatusVersion)+"\n", Version)
			fmt.Printf(i18n.TL(i18n.KeyStatusBuild)+"\n", BuildTime)
			fmt.Println()

			// LLM status
			if cfg.LLMConfigured() {
				fmt.Println(i18n.TL(i18n.KeyStatusLLM))
			} else {
				fmt.Println(i18n.TL(i18n.KeyStatusLLMNotCfg))
			}

			// LLM Limits status
			if cfg.LLM.Limits.Enabled {
				fmt.Println()
				fmt.Println("LLM Limits:")
				if cfg.LLM.Limits.SchedulerEnabled {
					fmt.Printf("  Scheduler: active\n")
				}
				if cfg.LLM.Limits.Requests.Daily.Max > 0 {
					fmt.Printf("  Requests (daily): %d / %d\n",
						0, cfg.LLM.Limits.Requests.Daily.Max)
				}
				if cfg.LLM.Limits.Requests.Weekly.Max > 0 {
					fmt.Printf("  Requests (weekly): %d / %d\n",
						0, cfg.LLM.Limits.Requests.Weekly.Max)
				}
				if cfg.LLM.Limits.Requests.Monthly.Max > 0 {
					fmt.Printf("  Requests (monthly): %d / %d\n",
						0, cfg.LLM.Limits.Requests.Monthly.Max)
				}
				if cfg.LLM.Limits.Tokens.Daily.InputMax > 0 {
					fmt.Printf("  Tokens Input (daily): %d / %d\n",
						0, cfg.LLM.Limits.Tokens.Daily.InputMax)
				}
				if cfg.LLM.Limits.Tokens.Daily.OutputMax > 0 {
					fmt.Printf("  Tokens Output (daily): %d / %d\n",
						0, cfg.LLM.Limits.Tokens.Daily.OutputMax)
				}
				if cfg.LLM.Limits.Concurrency.MaxRunning > 0 {
					fmt.Printf("  Concurrency: %d / %d\n",
						0, cfg.LLM.Limits.Concurrency.MaxRunning)
				}
				fmt.Printf("  Enforcement: %s\n", cfg.LLM.Limits.Enforcement)
			}

			// Health endpoint
			if cfg.Health.Enabled {
				addr := cfg.Health.Addr
				client := &http.Client{Timeout: 3 * time.Second}
				resp, err := client.Get(fmt.Sprintf("http://%s/health", addr))
				if err != nil {
					fmt.Printf(i18n.TL(i18n.KeyStatusHealthUnreach)+"\n", err)
				} else {
					defer resp.Body.Close()
					body, _ := io.ReadAll(resp.Body)
					fmt.Printf(i18n.TL(i18n.KeyStatusHealthOK)+"\n", body)
				}
			} else {
				fmt.Println(i18n.TL(i18n.KeyStatusHealthDisabled))
			}

			// Metrics endpoint
			if cfg.Metrics.Enabled {
				fmt.Printf(i18n.TL(i18n.KeyStatusMetricsEnabled)+"\n", cfg.Metrics.Addr)
			} else {
				fmt.Println(i18n.TL(i18n.KeyStatusMetricsDisabled))
			}

			// Transports
			fmt.Println()
			fmt.Println(i18n.TL(i18n.KeyStatusTransports))
			if cfg.Transport.Stdio.Enabled {
				fmt.Println(i18n.TL(i18n.KeyStatusTransStdio))
			}
			if cfg.Transport.SSH.Enabled {
				addr := cfg.Transport.SSH.Addr
				if addr == "" {
					addr = ":2222"
				}
				fmt.Printf(i18n.TL(i18n.KeyStatusTransSSH)+"\n", addr)
			}
			if cfg.Transport.MQTT.Enabled {
				fmt.Printf(i18n.TL(i18n.KeyStatusTransMQTT)+"\n", cfg.Transport.MQTT.Broker)
			}
			if cfg.Transport.Email.Enabled {
				fmt.Printf(i18n.TL(i18n.KeyStatusTransEmail)+"\n", cfg.Transport.Email.From)
			}

			// Shell tool mode
			fmt.Println()
			if len(cfg.MCP.Shell.AllowedCommands) > 0 {
				fmt.Printf(i18n.TL(i18n.KeyStatusShellRestricted)+"\n", cfg.MCP.Shell.AllowedCommands)
			} else {
				fmt.Println(i18n.TL(i18n.KeyStatusShellUnrestricted))
			}

			return nil
		},
	}
}
