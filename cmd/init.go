package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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
	cfgPath := config.ProjectConfigDir + "/" + config.ConfigFileName + ".yaml"
	cfgExists := false
	if _, err := os.Stat(cfgPath); err == nil {
		cfgExists = true
	}

	// Generate config if missing
	if !cfgExists {
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
	}

	// Git init .dolphin (idempotent)
	if err := gitInitDotDolphin(config.ProjectConfigDir); err != nil {
		fmt.Fprintf(os.Stderr, "git init .dolphin: %v\n", err)
	}

	return nil
}

func gitInitDotDolphin(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil // already initialized
	}

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("init: %w", err)
	}

	gitignore := "# dolphin version control\nconfig.yaml\nlogs/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0600); err != nil {
		return fmt.Errorf(".gitignore: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if _, err := wt.Add("."); err != nil {
		return fmt.Errorf("add: %w", err)
	}
	if _, err := wt.Commit("dolphin init", &git.CommitOptions{
		Author: &object.Signature{Name: "dolphin", Email: "dolphin@localhost", When: time.Now()},
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
