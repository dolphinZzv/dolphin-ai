package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/i18n"
	"dolphin/internal/lifecycle"
	"github.com/spf13/cobra"
)

func main() {
	var configPath string

	rootCmd := &cobra.Command{
		Use:   "dolphin",
		Short: "Dolphin — AI agent",
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				cmd.PrintErrln("config:", err)
				os.Exit(1)
			}

			if lang := cfg.GetString("lang"); lang == "" {
				cfg.Set("lang", config.DetectLang())
			}
			i18n.SetLang(cfg.GetString("lang"))

			p := lifecycle.New(cfg)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			p.Start(ctx)

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			p.Shutdown()
		},
	}

	rootCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "path to config file")

	configCmd := &cobra.Command{Use: "config"}
	configCmd.AddCommand(command.NewConfigInitCmd())
	rootCmd.AddCommand(configCmd)

	rootCmd.Execute()
}
