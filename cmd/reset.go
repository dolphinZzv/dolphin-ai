package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdResetUse),
		Short: i18n.TL(i18n.KeyCmdResetShort),
		Long:  i18n.TL(i18n.KeyCmdResetLong),
		RunE:  runReset,
	}

	cmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")

	return cmd
}

func runReset(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	targets := cleanupTargets()

	// Show what will be removed
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyResetWillRemove))
	listTargets(targets)

	// Confirm
	if !force {
		if !confirmRemoval("reset") {
			return nil
		}
	}

	fmt.Fprintln(os.Stderr)
	removed, errors := doRemove(targets)

	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyResetComplete), removed))
	if errors > 0 {
		fmt.Fprintf(os.Stderr, ", %d errors", errors)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyResetMarkerReset))
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyResetRunAgain))

	return nil
}

// cleanupTargets builds the list of paths to remove for a dolphin reset.
// Config files (config.yaml) are never removed.
func cleanupTargets() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	userDolphinDir := filepath.Join(homeDir, config.UserConfigDir)
	projectDolphinDir := config.ProjectConfigDir

	return []string{
		config.FirstRunMarker(),
		config.EmailConfiguredMarker(),
		config.DolphinIDFile(),
		filepath.Join(userDolphinDir, "ssh_password"),
		filepath.Join(userDolphinDir, "SYSTEM.md"),
		filepath.Join(userDolphinDir, "cache"),
		filepath.Join(userDolphinDir, "skills"),
		filepath.Join(userDolphinDir, "commands"),
		filepath.Join(userDolphinDir, "plugins"),
		filepath.Join(projectDolphinDir, "sessions"),
		filepath.Join(projectDolphinDir, "diary"),
		filepath.Join(projectDolphinDir, "workspaces"),
		filepath.Join(projectDolphinDir, "logs"),
		filepath.Join(projectDolphinDir, "CRONTAB.md"),
		filepath.Join(projectDolphinDir, "skills"),
		filepath.Join(projectDolphinDir, "commands"),
		config.SystemConfigDir,
	}
}

// listTargets prints each target with its type (directory or file).
func listTargets(targets []string) {
	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  - %s"+i18n.TL(i18n.KeyNotExistSkip)+"\n", t)
			continue
		}
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "  - %s"+i18n.TL(i18n.KeyDirectory)+"\n", t)
		} else {
			fmt.Fprintf(os.Stderr, "  - %s\n", t)
		}
	}
}

// confirmRemoval asks the user for confirmation. Returns true if confirmed.
func confirmRemoval(action string) bool {
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyResetConfirm))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input != "y" && input != "yes" {
		actionLabel := strings.ToUpper(action[:1]) + action[1:]
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyResetCancelled)+"\n", actionLabel))
		return false
	}
	return true
}

// doRemove removes all given targets and returns counts.
// Prints each item with its status (removed, skipped).
func doRemove(targets []string) (removed, errors int) {
	for _, t := range targets {
		if _, err := os.Stat(t); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "  - %s (skipped, not found)\n", t)
			continue
		}
		if err := os.RemoveAll(t); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s (error: %v)\n", t, err)
			errors++
		} else {
			fmt.Fprintf(os.Stderr, "  ✓ %s\n", t)
			removed++
		}
	}
	return
}
