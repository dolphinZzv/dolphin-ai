package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/skill"

	"github.com/spf13/cobra"
)

type skillResult struct {
	Name        string
	Description string
	Source      string // "local" or repo name
	Installed   bool
}

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
	cfg, mgr, err := loadSkillsCmdConfig()
	if err != nil {
		return err
	}

	query := strings.ToLower(args[0])
	installed := mgr.List()

	// Build results, de-duplicating by name
	seen := make(map[string]bool)
	var results []skillResult

	// Local skills first
	for _, s := range installed {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Description), query) {
			results = append(results, skillResult{
				Name:        s.Name,
				Description: s.Description,
				Source:      "local",
				Installed:   true,
			})
			seen[s.Name] = true
		}
	}

	// Fetch remote repos and search their manifests
	if len(cfg.Skills.Repos) > 0 {
		homeDir, err := os.UserHomeDir()
		cacheDir := ""
		if err == nil {
			cacheDir = filepath.Join(homeDir, config.UserConfigDir, "cache")
		}
		fetcher := config.NewRepoFetcher(cacheDir)
		if ex, err := os.Executable(); err == nil {
			fetcher.SetLocalDir(filepath.Dir(ex))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		manifests := fetcher.FetchAll(ctx, cfg.Skills.Repos)
		cancel()

		for _, m := range manifests {
			for _, t := range m.Tools {
				if seen[t.Name] {
					continue
				}
				haystack := strings.ToLower(t.Name + " " + t.Description)
				if strings.Contains(haystack, query) {
					results = append(results, skillResult{
						Name:        t.Name,
						Description: t.Description,
						Source:      m.Name,
						Installed:   false,
					})
					seen[t.Name] = true
				}
			}
		}
	}

	if len(results) == 0 {
		fmt.Printf("No skills found matching %q.\n", args[0])
		return nil
	}

	fmt.Printf("%-30s %-18s %s\n", "NAME", "SOURCE", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range results {
		desc := r.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		mark := " "
		if r.Installed {
			mark = "*"
		}
		fmt.Printf("%s%-29s %-18s %s\n", mark, r.Name, r.Source, desc)
	}
	fmt.Printf("\nFound %d results matching %q (* = installed).\n", len(results), args[0])
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
