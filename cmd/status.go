package cmd

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"dolphin/internal/config"

	"github.com/spf13/cobra"
)

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show dolphin daemon health and configuration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			fmt.Printf("Version: %s\n", Version)
			fmt.Printf("Build: %s\n", BuildTime)
			fmt.Println()

			// LLM status
			if cfg.LLMConfigured() {
				fmt.Println("LLM:       configured")
			} else {
				fmt.Println("LLM:       NOT configured (run 'dolphin setup')")
			}

			// Health endpoint
			if cfg.Health.Enabled {
				addr := cfg.Health.Addr
				client := &http.Client{Timeout: 3 * time.Second}
				resp, err := client.Get(fmt.Sprintf("http://%s/health", addr))
				if err != nil {
					fmt.Printf("Health:    unreachable (%v)\n", err)
				} else {
					defer resp.Body.Close()
					body, _ := io.ReadAll(resp.Body)
					fmt.Printf("Health:    OK — %s\n", body)
				}
			} else {
				fmt.Println("Health:    disabled (set health.enabled=true)")
			}

			// Metrics endpoint
			if cfg.Metrics.Enabled {
				fmt.Printf("Metrics:   enabled at %s\n", cfg.Metrics.Addr)
			} else {
				fmt.Println("Metrics:   disabled")
			}

			// Transports
			fmt.Println()
			fmt.Println("Transports:")
			if cfg.Transport.Stdio.Enabled {
				fmt.Println("  - stdio: enabled")
			}
			if cfg.Transport.SSH.Enabled {
				addr := cfg.Transport.SSH.Addr
				if addr == "" {
					addr = ":2222"
				}
				fmt.Printf("  - ssh:   enabled at %s\n", addr)
			}
			if cfg.Transport.MQTT.Enabled {
				fmt.Printf("  - mqtt:  enabled (broker: %s)\n", cfg.Transport.MQTT.Broker)
			}
			if cfg.Transport.Email.Enabled {
				fmt.Printf("  - email: enabled (from: %s)\n", cfg.Transport.Email.From)
			}

			// Shell tool mode
			fmt.Println()
			if len(cfg.MCP.Shell.AllowedCommands) > 0 {
				fmt.Printf("Shell:    restricted (allowed: %v)\n", cfg.MCP.Shell.AllowedCommands)
			} else {
				fmt.Println("Shell:    unrestricted (pipes and redirects enabled)")
			}

			return nil
		},
	}
}
