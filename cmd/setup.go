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
	skillNames := profile.Skills
	for _, s := range extraSkills {
		skillNames = append(skillNames, s.Name)
	}
	mcpNames := profile.MCP
	for _, m := range extraMCP {
		mcpNames = append(mcpNames, m.Name)
	}

	fmt.Fprintf(os.Stderr, "\n=== Recommended tools for %s ===\n", profile.Description)
	if len(skillNames) > 0 {
		fmt.Fprintf(os.Stderr, "Skills: %s\n", strings.Join(skillNames, ", "))
	}
	if len(mcpNames) > 0 {
		fmt.Fprintf(os.Stderr, "MCP:    %s\n", strings.Join(mcpNames, ", "))
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
			Skills: skillNames,
			MCP:    mcpNames,
		}
		if err := config.SaveToolSelection(sel, "project"); err != nil {
			return fmt.Errorf("save to project config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nTools saved to .dolphin/config.yaml\n")

	case choice == "a":
		sel := &config.ToolSelection{
			Skills: skillNames,
			MCP:    mcpNames,
		}
		if err := config.SaveToolSelection(sel, "user"); err != nil {
			return fmt.Errorf("save to user config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\nTools saved to ~/.dolphin/config.yaml\n")

	default:
		fmt.Fprintf(os.Stderr, "\nSkipped. No changes made.\n")
		fmt.Fprintf(os.Stderr, "You can add tools manually in your config or skill/MCP repos.\n")
	}

	// Summary
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "=== Setup Complete ===")
	fmt.Fprintf(os.Stderr, "  Profile: %s\n", profile.Description)
	fmt.Fprintf(os.Stderr, "  Skills:  %d\n", len(skillNames))
	fmt.Fprintf(os.Stderr, "  MCP:     %d\n", len(mcpNames))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  1. Set your LLM API key: export DZ_LLM_API_KEY=sk-...")
	fmt.Fprintln(os.Stderr, "  2. Restart dolphin for changes to take effect")
	if choice == "p" || choice == "a" {
		fmt.Fprintln(os.Stderr, "  3. Run 'dolphin doctor' to verify your setup")
	}
	fmt.Fprintln(os.Stderr)

	return nil
}
