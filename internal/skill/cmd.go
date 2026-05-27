package skill

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type skillResult struct {
	Name        string
	Description string
	Source      string // "local" or repo name
	Installed   bool
}

// SkillsCommand returns a cobra command that loads skills from config.
func SkillsCommand() *cobra.Command {
	return newSkillsCommand(nil, "")
}

// SkillsCommandWithConfigPath returns a skills command that uses the given
// config file path instead of default paths.
func SkillsCommandWithConfigPath(cfgPath string) *cobra.Command {
	return newSkillsCommand(nil, cfgPath)
}

// SkillsCommandWithManager returns a cobra command that uses the given Manager.
// When mgr is nil, handlers load from config automatically.
func SkillsCommandWithManager(mgr *Manager) *cobra.Command {
	return newSkillsCommand(mgr, "")
}

func newSkillsCommand(mgr *Manager, cfgPath string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsUse),
		Short: i18n.TL(i18n.KeyCmdSkillsShort),
		RunE: func(c *cobra.Command, _ []string) error {
			return runSkillsList(c, loadSkillManager(mgr, cfgPath))
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsListUse),
		Short: i18n.TL(i18n.KeyCmdSkillsListShort),
		RunE: func(c *cobra.Command, _ []string) error {
			return runSkillsList(c, loadSkillManager(mgr, cfgPath))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsSearchUse),
		Short: i18n.TL(i18n.KeyCmdSkillsSearchShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsSearch(c, loadSkillManager(mgr, cfgPath), args[0], cfgPath)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsInstallUse),
		Short: i18n.TL(i18n.KeyCmdSkillsInstallShort),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsInstall(c, args, cfgPath)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsNewUse),
		Short: i18n.TL(i18n.KeyCmdSkillsNewShort),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsNew(c, args, mgr, cfgPath)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsDisableUse),
		Short: i18n.TL(i18n.KeyCmdSkillsDisableShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsDisable(c, loadSkillManager(mgr, cfgPath), args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsEnableUse),
		Short: i18n.TL(i18n.KeyCmdSkillsEnableShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsEnable(c, loadSkillManager(mgr, cfgPath), args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     i18n.TL(i18n.KeyCmdSkillsUninstallUse),
		Aliases: []string{"delete"},
		Short:   i18n.TL(i18n.KeyCmdSkillsUninstallShort),
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsUninstall(c, loadSkillManager(mgr, cfgPath), args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSkillsShowUse),
		Short: i18n.TL(i18n.KeyCmdSkillsShowShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runSkillsShow(c, loadSkillManager(mgr, cfgPath), args[0])
		},
	})

	return cmd
}

// loadSkillManager returns the provided manager or loads from config.
func loadSkillManager(mgr *Manager, cfgPath string) *Manager {
	if mgr != nil {
		return mgr
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil
	}
	skillDirs := []string{cfg.Skills.Dir}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userSkillsDir := filepath.Join(homeDir, config.UserConfigDir, "skills")
		if userSkillsDir != cfg.Skills.Dir {
			skillDirs = append([]string{userSkillsDir}, skillDirs...)
		}
	}
	m := NewManager(skillDirs...)
	if err := m.Load(); err != nil {
		zap.S().Warnw("failed to load skills", "error", err)
	}
	return m
}

func runSkillsList(cmd *cobra.Command, mgr *Manager) error {
	if mgr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsNotAvail))
		return nil
	}
	skills := mgr.List()
	if len(skills) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLINone))
		return nil
	}

	zap.S().Infow("listed skills", "count", len(skills))

	fmt.Fprintf(cmd.OutOrStdout(), "%-30s %s\n", "NAME", "DESCRIPTION")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 80))
	for _, s := range skills {
		desc := s.Description
		if len(desc) > 45 {
			desc = desc[:42] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-30s %s\n", s.Name, desc)
	}
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLITotal)+"\n", len(skills))
	return nil
}

func runSkillsSearch(cmd *cobra.Command, mgr *Manager, query string, cfgPath string) error {
	if mgr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLISearchNone), query)
		return nil
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	q := strings.ToLower(query)
	installed := mgr.List()

	seen := make(map[string]bool)
	var results []skillResult

	for _, s := range installed {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) {
			results = append(results, skillResult{
				Name:        s.Name,
				Description: s.Description,
				Source:      "local",
				Installed:   true,
			})
			seen[s.Name] = true
		}
	}

	repos := cfg.Skills.Repos
	if len(repos) == 0 {
		repos = []string{"https://raw.githubusercontent.com/dolphinZzv/dolphin/main/skills.json"}
	}

	if len(repos) > 0 {
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
		var manifests []*config.ToolManifest
		for _, repo := range repos {
			m, err := fetcher.FetchSkillsManifest(ctx, repo)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "[skills] fetch %s: %v\n", repo, err)
				continue
			}
			manifests = append(manifests, m)
		}
		cancel()

		for _, m := range manifests {
			for _, t := range m.Tools {
				if seen[t.Name] {
					continue
				}
				haystack := strings.ToLower(t.Name + " " + t.Description)
				if strings.Contains(haystack, q) {
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

	zap.S().Infow("searched skills", "query", query, "results", len(results))

	if len(results) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLISearchNone)+"\n", query)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-18s %s\n", "NAME", "SOURCE", "DESCRIPTION")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 80))
	for _, r := range results {
		desc := r.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		mark := " "
		if r.Installed {
			mark = "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s%-29s %-18s %s\n", mark, r.Name, r.Source, desc)
	}
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIFound)+"\n", len(results), query)
	return nil
}

func runSkillsInstall(cmd *cobra.Command, args []string, cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	name := args[0]
	installDir := cfg.Skills.Dir
	mgr := NewManager(installDir)
	if err := mgr.Load(); err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	if _, ok := mgr.Get(name); ok {
		return fmt.Errorf("skill %q is already installed", name)
	}
	if _, err := os.Stat(filepath.Join(installDir, name+".disabled")); err == nil {
		return fmt.Errorf("skill %q is installed but disabled. Use 'dolphin skills enable %s' to restore it", name, name)
	}

	repos := cfg.Skills.Repos
	if len(repos) == 0 {
		repos = []string{"https://raw.githubusercontent.com/dolphinZzv/dolphin/main/skills.json"}
	}

	homeDir, err := os.UserHomeDir()
	cacheDir := ""
	if err == nil {
		cacheDir = filepath.Join(homeDir, config.UserConfigDir, "cache")
	}
	fetcher := config.NewRepoFetcher(cacheDir)
	if ex, err := os.Executable(); err == nil {
		fetcher.SetLocalDir(filepath.Dir(ex))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var found *config.ToolEntry
	for _, repo := range repos {
		m, err := fetcher.FetchSkillsManifest(ctx, repo)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "[skills] fetch %s: %v\n", repo, err)
			continue
		}
		for _, t := range m.Tools {
			if t.Name == name {
				found = &t
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		return fmt.Errorf("skill %q not found in any configured repo", name)
	}

	description := found.Description
	if len(args) > 1 {
		description = args[1]
	}

	var content string
	if found.URL != "" {
		content, err = downloadSkillContent(found.URL)
		if err != nil {
			return fmt.Errorf("download skill content: %w", err)
		}
	} else {
		content = fmt.Sprintf("# %s\n\n%s\n", name, description)
	}

	if err := mgr.Register(name, description, content); err != nil {
		return fmt.Errorf("install skill: %w", err)
	}

	zap.S().Infow("installed skill", "name", name, "dir", installDir)
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIInstalled)+"\n", name, installDir)
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIEdit))
	return nil
}

func downloadSkillContent(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return string(data), nil
}

func runSkillsNew(cmd *cobra.Command, args []string, mgr *Manager, cfgPath string) error {
	name := args[0]
	description := name
	if len(args) > 1 {
		description = args[1]
	}

	if mgr == nil {
		cfg, err := config.Load("")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		newMgr := NewManager(cfg.Skills.Dir)
		if err := newMgr.Load(); err != nil {
			return fmt.Errorf("load skills: %w", err)
		}
		mgr = newMgr
	}

	if err := mgr.NewTemplate(name, description); err != nil {
		return fmt.Errorf("create skill: %w", err)
	}

	zap.S().Infow("created skill", "name", name, "dir", mgr.Dir())
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLICreated)+"\n", name, mgr.Dir())
	fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIEdit))
	return nil
}

func runSkillsDisable(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("skills system not available")
	}
	if err := mgr.Disable(name); err != nil {
		return fmt.Errorf("disable skill: %w", err)
	}
	zap.S().Infow("disabled skill", "name", name)
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIDisabled)+"\n", name)
	return nil
}

func runSkillsEnable(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("skills system not available")
	}
	if err := mgr.Enable(name); err != nil {
		return fmt.Errorf("enable skill: %w", err)
	}
	zap.S().Infow("enabled skill", "name", name)
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIEnabled)+"\n", name)
	return nil
}

func runSkillsUninstall(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("skills system not available")
	}
	if _, ok := mgr.Get(name); !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	if err := mgr.Unregister(name); err != nil {
		return fmt.Errorf("uninstall skill: %w", err)
	}
	zap.S().Infow("uninstalled skill", "name", name)
	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySkillsCLIUninstalled)+"\n", name)
	return nil
}

func runSkillsShow(cmd *cobra.Command, mgr *Manager, name string) error {
	if mgr == nil {
		return fmt.Errorf("skills system not available")
	}
	s, ok := mgr.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "--- %s ---\n", s.Name)
	fmt.Fprintln(cmd.OutOrStdout(), s.Content)
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
