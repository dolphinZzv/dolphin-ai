package cmd

import (
	"fmt"
	"os"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSetupUse),
		Short: i18n.TL(i18n.KeyCmdSetupShort),
		Long:  i18n.TL(i18n.KeyCmdSetupLong),
		RunE:  runSetup,
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
		//nolint:govet
		if err := config.MarkFirstRunDone(); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset first-run marker: %w", err)
		}
		fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupFirstRunReset))
		return nil
	}

	// Run the career selection prompt
	profile, err := config.RunFirstRunPrompt()
	if err != nil {
		return fmt.Errorf("career prompt: %w", err)
	}

	if profile == nil {
		fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupSkipped))
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

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeySetupRecTools)+"\n", profile.Description))
	if len(skillNames) > 0 {
		fmt.Fprintf(os.Stderr, "Skills: %s\n", strings.Join(skillNames, ", "))
	}
	if len(mcpNames) > 0 {
		fmt.Fprintf(os.Stderr, "MCP:    %s\n", strings.Join(mcpNames, ", "))
	}

	// Ask where to save
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupLoadTo)+"\n")
	fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupChoice))

	var choice string
	_, _ = fmt.Scanln(&choice)
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
		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupSavedProject)+"\n")

	case choice == "a":
		sel := &config.ToolSelection{
			Skills: skillNames,
			MCP:    mcpNames,
		}
		if err := config.SaveToolSelection(sel, "user"); err != nil {
			return fmt.Errorf("save to user config: %w", err)
		}
		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupSavedGlobal)+"\n")

	default:
		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupSkipNoChange)+"\n")
		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeySetupManual)+"\n")
	}

	// Summary
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupComplete))
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeySetupProfile)+"\n", profile.Description))
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeySetupSkillsCount)+"\n", len(skillNames)))
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeySetupMCPCount)+"\n", len(mcpNames)))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupNextSteps))
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupStep1))
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupStep2))
	if choice == "p" || choice == "a" {
		fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeySetupStep3))
	}
	fmt.Fprintln(os.Stderr)

	return nil
}
