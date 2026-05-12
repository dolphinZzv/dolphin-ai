package cmd

import (
	"fmt"
	"os"
	"strings"

	"dolphin/internal/config"

	"github.com/spf13/cobra"
)

func NewSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Re-run the career-guided tool setup wizard",
		Long: `Re-runs the career selection prompt and displays recommended tools.

The first-run marker is NOT reset, so this does not trigger on next startup.
Use --reset to clear the first-run marker and start fresh.`,
		RunE: runSetup,
	}

	cmd.Flags().Bool("reset", false, "reset the first-run marker (prompt on next startup)")
	cmd.Flags().StringP("config", "c", "", "path to config file")

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Handle --reset flag
	if reset, _ := cmd.Flags().GetBool("reset"); reset {
		if err := config.MarkFirstRunDone(); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset first-run marker: %w", err)
		}
		fmt.Fprintln(os.Stderr, "First-run marker reset. Career prompt will show on next startup.")
		return nil
	}

	// Run the career selection prompt
	profile, err := config.RunFirstRunPrompt()
	if err != nil {
		return fmt.Errorf("career prompt: %w", err)
	}

	if profile == nil {
		fmt.Fprintln(os.Stderr, "\nSetup skipped. No changes made.")
		return nil
	}

	// Augment with repo tools
	extraSkills, extraMCP := config.AugmentWithRepos(profile, cfg.Skills.Repos, cfg.MCP.Repos)
	allSkills := append([]string{}, profile.Skills...)
	allSkills = append(allSkills, extraSkills...)
	allMCP := append([]string{}, profile.MCP...)
	allMCP = append(allMCP, extraMCP...)

	fmt.Fprintf(os.Stderr, "\n=== Recommended tools for %s ===\n", profile.Description)
	if len(allSkills) > 0 {
		fmt.Fprintf(os.Stderr, "Skills: %s\n", strings.Join(allSkills, ", "))
	}
	if len(allMCP) > 0 {
		fmt.Fprintf(os.Stderr, "MCP:    %s\n", strings.Join(allMCP, ", "))
	}

	// Ask where to save
	fmt.Fprintf(os.Stderr, "\nLoad to: [p] project  [a] global  [n] skip\n")
	fmt.Fprintf(os.Stderr, "Choice: ")

	var choice string
	fmt.Scanln(&choice)
	choice = strings.TrimSpace(strings.ToLower(choice))

	switch {
	case choice == "p":
		sel := &config.ToolSelection{
			Skills: allSkills,
			MCP:    allMCP,
		}
		if err := config.SaveToolSelection(sel, "project"); err != nil {
			return fmt.Errorf("save to project config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nTools saved to .dolphin/config.yaml\n")
		fmt.Fprintf(os.Stderr, "Restart dolphin for changes to take effect.\n\n")

	case choice == "a":
		sel := &config.ToolSelection{
			Skills: allSkills,
			MCP:    allMCP,
		}
		if err := config.SaveToolSelection(sel, "user"); err != nil {
			return fmt.Errorf("save to user config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nTools saved to ~/.dolphin/config.yaml\n")
		fmt.Fprintf(os.Stderr, "Restart dolphin for changes to take effect.\n\n")

	default:
		fmt.Fprintf(os.Stderr, "\nSkipped. No changes made.\n")
		fmt.Fprintf(os.Stderr, "You can add tools manually in your config or skill/MCP repos.\n\n")
	}

	return nil
}
