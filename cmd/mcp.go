package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type mcpResult struct {
	Name        string
	Description string
	Source      string // repo name
	Installed   bool
}

func NewMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPUse),
		Short: i18n.TL(i18n.KeyCmdMCPShort),
		RunE:  runMCPList,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPSearchUse),
		Short: i18n.TL(i18n.KeyCmdMCPSearchShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runMCPSearch,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPInstallUse),
		Short: i18n.TL(i18n.KeyCmdMCPInstallShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runMCPInstall,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPUninstallUse),
		Short: i18n.TL(i18n.KeyCmdMCPUninstallShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runMCPUninstall,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPEnableUse),
		Short: i18n.TL(i18n.KeyCmdMCPEnableShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runMCPEnable,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdMCPDisableUse),
		Short: i18n.TL(i18n.KeyCmdMCPDisableShort),
		Args:  cobra.ExactArgs(1),
		RunE:  runMCPDisable,
	})

	return cmd
}

func loadMCPCmdConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func runMCPList(cmd *cobra.Command, args []string) error {
	cfg, err := loadMCPCmdConfig()
	if err != nil {
		return err
	}

	if len(cfg.MCP.Servers) == 0 {
		fmt.Println(i18n.TL(i18n.KeyMCPCLINone))
		return nil
	}

	zap.S().Infow("listed mcp servers", "count", len(cfg.MCP.Servers))

	fmt.Printf("%-30s %s\n", "NAME", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for name, srv := range cfg.MCP.Servers {
		desc := srv.Command
		if desc == "" {
			desc = srv.URL
		}
		if len(desc) > 45 {
			desc = desc[:42] + "..."
		}
		fmt.Printf("%-30s %s\n", name, desc)
	}
	fmt.Printf(i18n.TL(i18n.KeyMCPCLITotal)+"\n", len(cfg.MCP.Servers))
	return nil
}

func runMCPSearch(cmd *cobra.Command, args []string) error {
	cfg, err := loadMCPCmdConfig()
	if err != nil {
		return err
	}

	query := strings.ToLower(args[0])

	// Build installed set
	installed := make(map[string]bool)
	for name := range cfg.MCP.Servers {
		installed[name] = true
	}

	var results []mcpResult

	// Fetch remote repos and search their manifests
	if len(cfg.MCP.Repos) > 0 {
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
		manifests := fetcher.FetchAll(ctx, cfg.MCP.Repos)
		cancel()

		seen := make(map[string]bool)
		for _, m := range manifests {
			for _, t := range m.Tools {
				if seen[t.Name] {
					continue
				}
				haystack := strings.ToLower(t.Name + " " + t.Description)
				if strings.Contains(haystack, query) {
					results = append(results, mcpResult{
						Name:        t.Name,
						Description: t.Description,
						Source:      m.Name,
						Installed:   installed[t.Name],
					})
					seen[t.Name] = true
				}
			}
		}
	}

	if len(results) == 0 {
		fmt.Printf(i18n.TL(i18n.KeyMCPCLISearchNone)+"\n", args[0])
		return nil
	}

	zap.S().Infow("searched mcp servers", "query", args[0], "results", len(results))

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
	fmt.Printf(i18n.TL(i18n.KeyMCPCLIFound)+"\n", len(results), args[0])
	return nil
}

func runMCPInstall(cmd *cobra.Command, args []string) error {
	cfg, err := loadMCPCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]

	// Check if already installed
	if _, exists := cfg.MCP.Servers[name]; exists {
		return fmt.Errorf("mcp server %q already installed", name)
	}

	// Fetch repos to find the server manifest
	if len(cfg.MCP.Repos) == 0 {
		return fmt.Errorf("no MCP repos configured — add repos to mcp.repos in config.yaml")
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
	manifests := fetcher.FetchAll(ctx, cfg.MCP.Repos)
	cancel()

	var found *config.ToolEntry
	for _, m := range manifests {
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
		return fmt.Errorf("mcp server %q not found in any repo", name)
	}

	if err := config.ApplyTools(nil, []config.ToolEntry{*found}); err != nil {
		return fmt.Errorf("install mcp server: %w", err)
	}

	zap.S().Infow("installed mcp server", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyMCPCLIInstalled)+"\n", name)
	return nil
}

func runMCPUninstall(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.RemoveMCPServer(name); err != nil {
		return fmt.Errorf("uninstall mcp server: %w", err)
	}

	zap.S().Infow("uninstalled mcp server", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyMCPCLIUninstalled)+"\n", name)
	return nil
}

func runMCPEnable(cmd *cobra.Command, args []string) error {
	cfg, err := loadMCPCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]
	if _, exists := cfg.MCP.Servers[name]; !exists {
		return fmt.Errorf("mcp server %q not found", name)
	}

	if err := config.ToggleMCPServer(name, true); err != nil {
		return fmt.Errorf("enable mcp server: %w", err)
	}

	zap.S().Infow("enabled mcp server", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyMCPCLIEnabled)+"\n", name)
	return nil
}

func runMCPDisable(cmd *cobra.Command, args []string) error {
	cfg, err := loadMCPCmdConfig()
	if err != nil {
		return err
	}

	name := args[0]
	if _, exists := cfg.MCP.Servers[name]; !exists {
		return fmt.Errorf("mcp server %q not found", name)
	}

	if err := config.ToggleMCPServer(name, false); err != nil {
		return fmt.Errorf("disable mcp server: %w", err)
	}

	zap.S().Infow("disabled mcp server", "name", name)
	fmt.Printf(i18n.TL(i18n.KeyMCPCLIDisabled)+"\n", name)
	return nil
}
