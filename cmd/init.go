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
		Use:   i18n.TL(i18n.KeyCmdInitUse),
		Short: i18n.TL(i18n.KeyCmdInitShort),
		Long: `Long:  i18n.TL(i18n.KeyCmdInitLong),
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(restrictive)
		},
	}

	cmd.Flags().BoolVarP(&restrictive, "restrictive", "r", false,
		i18n.TL(i18n.KeyInitRestrictiveFlag))

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
			fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyInitRestrictiveGenerated)+"\n", fp))
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveDiffs)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveShell)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveCDP)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveWebhook)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveLog)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictivePlugins)+"\n")
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitRestrictiveRun)+"\n")
		} else {
			fp, err = config.GenerateConfigFile(lang)
			if err != nil {
				return fmt.Errorf("generate config: %w", err)
			}
			fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyInitDefaultGenerated)+"\n", fp))
			fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyInitEditAndRun)+"\n")
		}
	}

	// Git init .dolphin (idempotent)
	if err := gitInitDotDolphin(config.ProjectConfigDir); err != nil {
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyInitGitError)+"\n", err))
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
	//nolint:govet
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0o600); err != nil {
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
