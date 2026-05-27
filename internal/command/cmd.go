package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

// CommandsCommand returns a cobra command that loads commands from config.
func CommandsCommand() *cobra.Command {
	return newCommandsCommand(nil)
}

// CommandsCommandWithManager returns a cobra command that uses the given Manager.
// When mgr is nil, handlers load from config automatically.
func CommandsCommandWithManager(mgr *Manager) *cobra.Command {
	return newCommandsCommand(mgr)
}

func newCommandsCommand(mgr *Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdCommandsUse),
		Short: i18n.TL(i18n.KeyCmdCommandsShort),
		RunE: func(c *cobra.Command, _ []string) error {
			return runCommandsList(c, loadCommandManager(mgr))
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdCommandsListUse),
		Short: i18n.TL(i18n.KeyCmdCommandsListShort),
		RunE: func(c *cobra.Command, _ []string) error {
			return runCommandsList(c, loadCommandManager(mgr))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdCommandsNewUse),
		Short: i18n.TL(i18n.KeyCmdCommandsNewShort),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			return runCommandsNew(c, args)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdCommandsDeleteUse),
		Short: i18n.TL(i18n.KeyCmdCommandsDeleteShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runCommandsDelete(c, loadCommandManager(mgr), args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdCommandsShowUse),
		Short: i18n.TL(i18n.KeyCmdCommandsShowShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runCommandsShow(c, loadCommandManager(mgr), args[0])
		},
	})

	return cmd
}

// loadCommandManager returns the provided manager or loads from config.
func loadCommandManager(mgr *Manager) *Manager {
	if mgr != nil {
		return mgr
	}
	cmdDirs := []string{filepath.Join(config.ProjectConfigDir, "commands")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userCmdDir := filepath.Join(homeDir, config.UserConfigDir, "commands")
		cmdDirs = append(cmdDirs, userCmdDir)
	}
	m := NewManager(cmdDirs...)
	if err := m.Load(); err != nil {
		return nil
	}
	return m
}

func runCommandsList(cmd *cobra.Command, mgr *Manager) error {
	if mgr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyCommandsNotAvail))
		return nil
	}
	cmds := mgr.List()
	if len(cmds) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyNoCommands))
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyNoCommandsHint))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", fmt.Sprintf(i18n.TL(i18n.KeyCommandHeader), "COMMAND", "DESCRIPTION"))
	fmt.Fprintln(cmd.OutOrStdout(), "------------------------------------------")
	for _, c := range cmds {
		fmt.Fprintf(cmd.OutOrStdout(), "/%-19s  %s\n", c.Name, c.Description)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyCommandRunHint))
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyCmdNewUsage))
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyCmdDeleteUsage))
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeyCmdShowUsage))
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

func runCommandsNew(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	name := args[0]
	description := name
	if len(args) > 1 {
		description = strings.Join(args[1:], " ")
	}

	// Build the same dir list as initCommandManager in root.go
	cmdDirs := []string{filepath.Join(config.ProjectConfigDir, "commands")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userCmdDir := filepath.Join(homeDir, config.UserConfigDir, "commands")
		cmdDirs = append(cmdDirs, userCmdDir)
	}
	mgr := NewManager(cmdDirs...)
	if err := mgr.Load(); err != nil {
		return fmt.Errorf("load commands: %w", err)
	}

	// Use dir from config if available
	primaryDir := cfg.Skills.Dir
	if primaryDir != "" {
		mgr = NewManager(primaryDir)
		if err := mgr.Load(); err != nil {
			return fmt.Errorf("load commands: %w", err)
		}
	}

	if err := mgr.NewTemplate(name, description); err != nil {
		return fmt.Errorf("create command: %w", err)
	}

	dir := mgr.Dir()
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeyCmdNewCreated)+"\n", name, dir)
	return nil
}

func runCommandsDelete(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("commands system not available")
	}
	if _, ok := mgr.Get(name); !ok {
		return fmt.Errorf("command %q not found", name)
	}
	if err := mgr.Unregister(name); err != nil {
		return fmt.Errorf("delete command: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeyCmdDeleteDone)+"\n", name)
	return nil
}

func runCommandsShow(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("commands system not available")
	}
	c, ok := mgr.Get(name)
	if !ok {
		return fmt.Errorf("command %q not found", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "--- %s ---\n", c.Name)
	fmt.Fprintln(cmd.OutOrStdout(), c.Content)
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
