package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type agentResult struct {
	Name        string
	Description string
	Source      string // "local" or repo name
	Installed   bool
	Disabled    bool
}

func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     i18n.TL(i18n.KeyCmdAgentUse),
		Aliases: []string{"agents"},
		Short:   i18n.TL(i18n.KeyCmdAgentShort),
		RunE:    runAgentList,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentListUse),
		Short: i18n.TL(i18n.KeyCmdAgentListShort),
		RunE:  runAgentList,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentSearchUse),
		Short: i18n.TL(i18n.KeyCmdAgentSearchShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentSearch,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentInstallUse),
		Short: i18n.TL(i18n.KeyCmdAgentInstallShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentInstall,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentNewUse),
		Short: i18n.TL(i18n.KeyCmdAgentNewShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentNew,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentDisableUse),
		Short: i18n.TL(i18n.KeyCmdAgentDisableShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentDisable,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentEnableUse),
		Short: i18n.TL(i18n.KeyCmdAgentEnableShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentEnable,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdAgentUninstallUse),
		Short: i18n.TL(i18n.KeyCmdAgentUninstallShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runAgentUninstall,
	})

	return cmd
}

func loadAgentCmdConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func agentDir() string {
	return filepath.Join(config.ProjectConfigDir, "agents")
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runAgentList(cmd *cobra.Command, args []string) error {
	agentsDir := agentDir()

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println(i18n.TL(i18n.KeyAgentCLINone))
			return nil
		}
		return fmt.Errorf("read agents dir: %w", err)
	}

	var active, disabled []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".disabled") {
			disabled = append(disabled, strings.TrimSuffix(name, ".disabled"))
		} else {
			active = append(active, name)
		}
	}

	if len(active) == 0 && len(disabled) == 0 {
		fmt.Println(i18n.TL(i18n.KeyAgentCLINone))
		return nil
	}

	zap.S().Infow("listed agents", "active", len(active), "disabled", len(disabled))

	fmt.Printf("%-30s %s\n", "NAME", "STATUS")
	fmt.Println(strings.Repeat("-", 50))
	for _, name := range active {
		fmt.Printf("%-30s %s\n", name, i18n.TL(i18n.KeyEnabled))
	}
	for _, name := range disabled {
		fmt.Printf("%-30s %s\n", name, i18n.TL(i18n.KeyDisabled))
	}
	fmt.Printf(i18n.TL(i18n.KeyAgentCLITotal)+"\n", len(active)+len(disabled))
	return nil
}

func runAgentSearch(cmd *cobra.Command, args []string) error {
	cfg, err := loadAgentCmdConfig()
	if err != nil {
		return err
	}

	query := strings.ToLower(args[0])

	// Build installed set from local agents dir
	agentsDir := agentDir()
	installed := make(map[string]bool)
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				name := strings.TrimSuffix(e.Name(), ".disabled")
				installed[name] = true
			}
		}
	}

	var results []agentResult

	// Determine repos: prefer agents.repos, fall back to skills.repos, then default
	repos := cfg.Agents.Repos
	if len(repos) == 0 {
		repos = cfg.Skills.Repos
	}
	if len(repos) == 0 {
		repos = []string{"https://raw.githubusercontent.com/dolphinZzv/dolphin/main/agents.json"}
	}

	// Fetch remote agent manifests
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
		manifests := fetcher.FetchAllAgentManifests(ctx, repos)
		cancel()

		seen := make(map[string]bool)
		for _, m := range manifests {
			for _, a := range m.Agents {
				if seen[a.Name] {
					continue
				}
				haystack := strings.ToLower(a.Name + " " + a.Description)
				if strings.Contains(haystack, query) {
					results = append(results, agentResult{
						Name:        a.Name,
						Description: a.Description,
						Source:      m.Name,
						Installed:   installed[a.Name],
					})
					seen[a.Name] = true
				}
			}
		}
	}

	zap.S().Infow("searched agents", "query", args[0], "results", len(results))

	if len(results) == 0 {
		fmt.Printf(i18n.TL(i18n.KeyAgentCLISearchNone)+"\n", args[0])
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
	fmt.Printf(i18n.TL(i18n.KeyAgentCLIFound)+"\n", len(results), args[0])
	return nil
}

func runAgentInstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadAgentCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]
	agentsDir := agentDir()

	// Check if already installed
	if _, err := os.Stat(filepath.Join(agentsDir, name, "agent.yaml")); err == nil {
		return fmt.Errorf("agent %q is already installed locally", name)
	}
	// Check if disabled
	if _, err := os.Stat(filepath.Join(agentsDir, name+".disabled", "agent.yaml")); err == nil {
		return fmt.Errorf("agent %q is installed but disabled. Use 'dolphin agent enable %s' to restore it", name, name)
	}

	// Determine repos: prefer agents.repos, fall back to skills.repos, then default
	repos := cfg.Agents.Repos
	if len(repos) == 0 {
		repos = cfg.Skills.Repos
	}
	if len(repos) == 0 {
		repos = []string{"https://raw.githubusercontent.com/dolphinZzv/dolphin/main/agents.json"}
	}

	// Fetch agents.json from repos
	if len(repos) == 0 {
		return fmt.Errorf("no repos configured — add repos to agents.repos or skills.repos in config.yaml")
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
	manifests := fetcher.FetchAllAgentManifests(ctx, repos)
	cancel()

	var found *config.AgentManifestEntry
	for _, m := range manifests {
		for _, a := range m.Agents {
			if a.Name == name {
				found = &a
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		return fmt.Errorf("agent %q not found in any configured repo", name)
	}

	// Download agent repo from URL (or copy local path)
	agentDir := filepath.Join(agentsDir, name)
	if err := downloadAgentRepo(found.URL, agentDir, found.Path); err != nil {
		return fmt.Errorf("download agent repo: %w", err)
	}

	// Verify agent.yaml exists
	agentYAML := filepath.Join(agentDir, "agent.yaml")
	if _, err := os.Stat(agentYAML); os.IsNotExist(err) {
		os.RemoveAll(agentDir)
		return fmt.Errorf("agent repo does not contain agent.yaml")
	}

	zap.S().Infow("installed agent", "name", name, "url", found.URL)
	fmt.Printf(i18n.TL(i18n.KeyAgentCLIInstalled)+"\n", name)
	return nil
}

func runAgentNew(cmd *cobra.Command, args []string) error {
	name := args[0]
	agentsDir := agentDir()
	agentPath := filepath.Join(agentsDir, name)

	// Check not already exists
	if _, err := os.Stat(filepath.Join(agentPath, "agent.yaml")); err == nil {
		return fmt.Errorf("agent %q already exists in %s", name, agentsDir)
	}

	// Create template directory
	if err := os.MkdirAll(agentPath, 0700); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	// Write agent.yaml template
	template := fmt.Sprintf(`name: %s
role: |
  You are a %s agent. Describe your role and capabilities.
tools:
  - shell
timeout: 120
`, name, name)

	if err := os.WriteFile(filepath.Join(agentPath, "agent.yaml"), []byte(template), 0600); err != nil {
		os.RemoveAll(agentPath)
		return fmt.Errorf("write agent.yaml: %w", err)
	}

	zap.S().Infow("created agent", "name", name, "dir", agentPath)
	fmt.Printf(i18n.TL(i18n.KeyAgentCLICreated)+"\n", name, agentPath)
	return nil
}

func runAgentDisable(cmd *cobra.Command, args []string) error {
	name := args[0]
	agentsDir := agentDir()
	agentPath := filepath.Join(agentsDir, name)
	disabledPath := filepath.Join(agentsDir, name+".disabled")

	// Check if agent exists
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		if _, err2 := os.Stat(disabledPath); err2 == nil {
			return fmt.Errorf("agent %q is already disabled", name)
		}
		return fmt.Errorf("agent %q not found", name)
	}

	// Rename to .disabled/
	if err := os.Rename(agentPath, disabledPath); err != nil {
		return fmt.Errorf("disable agent: %w", err)
	}

	zap.S().Infow("disabled agent", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyAgentCLIDisabled)+"\n", name)
	return nil
}

func runAgentEnable(cmd *cobra.Command, args []string) error {
	name := args[0]
	agentsDir := agentDir()
	agentPath := filepath.Join(agentsDir, name)
	disabledPath := filepath.Join(agentsDir, name+".disabled")

	// Check disabled dir exists
	if _, err := os.Stat(disabledPath); os.IsNotExist(err) {
		if _, err2 := os.Stat(agentPath); err2 == nil {
			return fmt.Errorf("agent %q is already enabled", name)
		}
		return fmt.Errorf("disabled agent %q not found", name)
	}

	// Rename back
	if err := os.Rename(disabledPath, agentPath); err != nil {
		return fmt.Errorf("enable agent: %w", err)
	}

	// Verify agent.yaml exists
	if _, err := os.Stat(filepath.Join(agentPath, "agent.yaml")); os.IsNotExist(err) {
		return fmt.Errorf("agent %q enabled but agent.yaml not found", name)
	}

	zap.S().Infow("enabled agent", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyAgentCLIEnabled)+"\n", name)
	return nil
}

func runAgentUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]
	agentsDir := agentDir()
	agentPath := filepath.Join(agentsDir, name)
	disabledPath := filepath.Join(agentsDir, name+".disabled")

	var targetDir string
	switch {
	case dirExists(agentPath):
		targetDir = agentPath
	case dirExists(disabledPath):
		targetDir = disabledPath
	default:
		return fmt.Errorf("agent %q not found", name)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("uninstall agent: %w", err)
	}

	zap.S().Infow("uninstalled agent", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyAgentCLIUninstalled)+"\n", name)
	return nil
}

// downloadAgentRepo downloads or copies an agent repo to the target directory.
func downloadAgentRepo(url, destDir, subPath string) error {
	if url == "" {
		return fmt.Errorf("no URL specified for agent repo")
	}

	// Local path
	if strings.HasPrefix(url, ".") || strings.HasPrefix(url, "/") || strings.HasPrefix(url, "~") {
		return copyAgentDir(url, destDir)
	}

	// SSH git URL (e.g. git@github.com:owner/repo.git)
	if strings.HasPrefix(url, "git@") {
		return gitCloneRepo(url, destDir)
	}

	// GitHub owner/repo format
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 && !strings.Contains(url, "://") && !strings.Contains(url, "\\") {
		return downloadGitHubRepo(url, destDir, subPath)
	}

	// Full URL
	return downloadGitHubRepo(url, destDir, subPath)
}

// gitCloneRepo clones a git repository using SSH URL.
func gitCloneRepo(repoURL, destDir string) error {
	cmd := exec.Command("git", "clone", repoURL, destDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return nil
}

// downloadGitHubRepo downloads a GitHub repo archive and extracts it.
func downloadGitHubRepo(repo, destDir, subPath string) error {
	// Handle both "owner/repo" and full URL formats
	if !strings.Contains(repo, "://") {
		repo = fmt.Sprintf("https://github.com/%s/archive/main.zip", repo)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(repo)
	if err != nil {
		return fmt.Errorf("download repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download repo: HTTP %d", resp.StatusCode)
	}

	// Read the ZIP into memory
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	// Find the root directory name in the zip (GitHub zips have a top-level dir)
	var rootPrefix string
	for _, f := range reader.File {
		if !f.FileInfo().IsDir() {
			parts := strings.SplitN(f.Name, "/", 2)
			if len(parts) == 2 {
				rootPrefix = parts[0] + "/"
			}
			break
		}
	}

	// Extract all files, stripping the root directory
	for _, f := range reader.File {
		var name string
		if rootPrefix != "" && strings.HasPrefix(f.Name, rootPrefix) {
			name = strings.TrimPrefix(f.Name, rootPrefix)
		} else {
			name = f.Name
		}
		if name == "" {
			continue
		}
		// Filter by subpath if specified
		if subPath != "" {
			if strings.HasPrefix(name, subPath+"/") {
				name = strings.TrimPrefix(name, subPath+"/")
			} else if name == subPath {
				continue
			} else {
				continue
			}
		}

		outPath := filepath.Join(destDir, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(outPath, 0700)
			continue
		}

		os.MkdirAll(filepath.Dir(outPath), 0700)
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open file in zip: %w", err)
		}

		out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create file: %w", err)
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return fmt.Errorf("extract file: %w", err)
		}
	}

	return nil
}

// copyAgentDir copies a local agent repo directory to the destination.
func copyAgentDir(src, destDir string) error {
	// Resolve ~ to home directory
	if strings.HasPrefix(src, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		src = filepath.Join(home, src[1:])
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory")
	}

	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)
		if fi.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0600)
	})
}
