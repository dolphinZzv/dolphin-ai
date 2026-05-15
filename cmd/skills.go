package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/skill"

	"github.com/spf13/cobra"
)

func NewSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List and manage skills",
		RunE:  runSkillsList,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all installed skills",
		RunE:  runSkillsList,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "search <query>",
		Short: "Search skills by name or description",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillsSearch,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "install <name> [description]",
		Short: "Install a new skill from boilerplate template",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runSkillsInstall,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "disable <name>",
		Short: "Disable and remove a skill",
		Args:  cobra.ExactArgs(1),
		RunE:  runSkillsDisable,
	})

	return cmd
}

func loadSkillsCmdConfig() (*config.Config, *skill.Manager, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	// Build skill dirs the same way as initSkillManager in root.go
	skillDirs := []string{cfg.Skills.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userSkillsDir := filepath.Join(homeDir, config.UserConfigDir, "skills")
		if userSkillsDir != cfg.Skills.Dir {
			skillDirs = append([]string{userSkillsDir}, skillDirs...)
		}
	}
	mgr := skill.NewManager(skillDirs...)
	if err := mgr.Load(); err != nil {
		return nil, nil, fmt.Errorf("load skills: %w", err)
	}
	return cfg, mgr, nil
}

func runSkillsList(cmd *cobra.Command, args []string) error {
	_, mgr, err := loadSkillsCmdConfig()
	if err != nil {
		return err
	}

	skills := mgr.List()
	if len(skills) == 0 {
		fmt.Println("No skills installed.")
		return nil
	}

	fmt.Printf("%-30s %s\n", "NAME", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range skills {
		desc := s.Description
		if len(desc) > 45 {
			desc = desc[:42] + "..."
		}
		fmt.Printf("%-30s %s\n", s.Name, desc)
	}
	fmt.Printf("\nTotal: %d skills\n", len(skills))
	return nil
}

func runSkillsSearch(cmd *cobra.Command, args []string) error {
	_, mgr, err := loadSkillsCmdConfig()
	if err != nil {
		return err
	}

	query := args[0]
	results := mgr.Search(query)
	if len(results) == 0 {
		fmt.Printf("No skills found matching %q.\n", query)
		return nil
	}

	fmt.Printf("%-30s %s\n", "NAME", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range results {
		desc := s.Description
		if len(desc) > 45 {
			desc = desc[:42] + "..."
		}
		fmt.Printf("%-30s %s\n", s.Name, desc)
	}
	fmt.Printf("\nFound %d skills matching %q.\n", len(results), query)
	return nil
}

func runSkillsInstall(cmd *cobra.Command, args []string) error {
	_, mgr, err := loadSkillsCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]
	description := name
	if len(args) > 1 {
		description = args[1]
	}

	if err := mgr.NewTemplate(name, description); err != nil {
		return fmt.Errorf("install skill: %w", err)
	}

	fmt.Printf("Skill %q installed in %s\n", name, mgr.Dir())
	fmt.Println("Edit the file to add your skill content.")
	return nil
}

func runSkillsDisable(cmd *cobra.Command, args []string) error {
	_, mgr, err := loadSkillsCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]
	if _, ok := mgr.Get(name); !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	if err := mgr.Unregister(name); err != nil {
		return fmt.Errorf("disable skill: %w", err)
	}

	fmt.Printf("Skill %q disabled and removed.\n", name)
	return nil
}
