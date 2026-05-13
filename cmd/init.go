package cmd

import (
	"fmt"
	"os"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewInitCmd() *cobra.Command {
	var restrictive bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a default config file",
		Long: `Generates a commented .dolphin/config.yaml with default settings.

Use --restrictive to generate a security-hardened config with:
  - Shell commands restricted to a safe allowlist
  - CDP browser automation disabled
  - Webhook tool disabled
  - Log level set to warn
  - Plugins disabled`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(restrictive)
		},
	}

	cmd.Flags().BoolVarP(&restrictive, "restrictive", "r", false,
		"generate security-hardened config (restricted shell, CDP/webhook disabled, warn log level)")

	return cmd
}

func runInit(restrictive bool) error {
	// Check if config already exists
	path := config.ProjectConfigDir + "/" + config.ConfigFileName + ".yaml"
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "Config already exists at %s.\n", path)
		fmt.Fprintf(os.Stderr, "Remove it first or edit manually.\n")
		return nil
	}

	lang := i18n.DetectLang()
	var fp string
	var err error

	if restrictive {
		fp, err = config.GenerateRestrictiveConfigFile(lang)
		if err != nil {
			return fmt.Errorf("generate restrictive config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nSecurity-hardened config generated: %s\n", fp)
		fmt.Fprintf(os.Stderr, "\nKey differences from default:\n")
		fmt.Fprintf(os.Stderr, "  - Shell: only allowlisted commands (ls, cat, grep, find, ...)\n")
		fmt.Fprintf(os.Stderr, "  - CDP browser: disabled\n")
		fmt.Fprintf(os.Stderr, "  - Webhook: disabled\n")
		fmt.Fprintf(os.Stderr, "  - Log level: warn\n")
		fmt.Fprintf(os.Stderr, "  - Plugins: disabled\n")
		fmt.Fprintf(os.Stderr, "\nRun 'dolphin' to start with this config.\n")
	} else {
		fp, err = config.GenerateConfigFile(lang)
		if err != nil {
			return fmt.Errorf("generate config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Default config generated: %s\n", fp)
		fmt.Fprintf(os.Stderr, "Edit it and run 'dolphin' to start.\n")
	}

	return nil
}
