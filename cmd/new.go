package cmd

import (
	"fmt"
	"os"

	"dolphin/internal/i18n"
	"github.com/spf13/cobra"
)

func NewNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdNewUse),
		Short: i18n.TL(i18n.KeyCmdNewShort),
		Long:  i18n.TL(i18n.KeyCmdNewLong),
		RunE:  runNew,
	}

	cmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")

	return cmd
}

func runNew(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	targets := cleanupTargets() // never removes config files

	// Show what will be removed
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyNewStarting))
	listTargets(targets)

	// Confirm
	if !force {
		if !confirmRemoval("new") {
			return nil
		}
	}

	fmt.Fprintln(os.Stderr)
	removed, errors := doRemove(targets)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, i18n.TL(i18n.KeyCleanupComplete), removed)
	if errors > 0 {
		fmt.Fprintf(os.Stderr, ", %d errors", errors)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)

	// Reset cfgFile so config.Load() doesn't try to load a stale -c path
	cfgFile = ""

	return runAgent(cmd, args)
}
