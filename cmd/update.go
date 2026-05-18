package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"dolphin/internal/update"

	"github.com/spf13/cobra"
)

func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [version]",
		Short: "Update dolphin to the latest or specified version from GitHub",
		Long: `Downloads and installs the specified version of dolphin from GitHub releases.

If no version tag is given, the latest release is used.
The version tag should match a GitHub release tag (e.g. "v1.0.0").

Examples:
  dolphin update          Update to the latest release
  dolphin update v1.0.0   Update to a specific version`,
		Args: cobra.MaximumNArgs(1),
		RunE: runUpdate,
	}

	cmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
	cmd.Flags().Bool("list", false, "list available versions and exit")

	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	listOnly, _ := cmd.Flags().GetBool("list")
	force, _ := cmd.Flags().GetBool("force")

	if listOnly {
		return listVersions()
	}

	version := ""
	if len(args) > 0 {
		version = args[0]
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
	}

	fmt.Fprintf(os.Stderr, "Current version: %s\n", Version)
	fmt.Fprintf(os.Stderr, "Platform: %s/%s\n\n", runtime.GOOS, runtime.GOARCH)

	client := update.NewGitHubClient()

	var release *update.Release
	var err error
	if version == "" {
		release, err = client.FetchLatest(cmd.Context(), "stable")
	} else {
		release, err = client.FetchRelease(cmd.Context(), version)
	}
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Release: %s\n", release.TagName)

	if Version == release.TagName {
		fmt.Fprintf(os.Stderr, "Already at version %s. No update needed.\n", release.TagName)
		return nil
	}

	asset, archiveName := update.FindAsset(release)
	if asset == nil {
		return fmt.Errorf("no release asset found for %s/%s (expected %q)", runtime.GOOS, runtime.GOARCH, archiveName)
	}

	if !force {
		fmt.Fprintf(os.Stderr, "\nReady to download and install %s (%s)\n", release.TagName, archiveName)
		fmt.Fprintf(os.Stderr, "Current binary: %s\n", update.MustExecPath())
		fmt.Fprintf(os.Stderr, "Are you sure? [y/N]: ")

		var input string
		_, _ = fmt.Scanln(&input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Update cancelled.")
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "\nDownloading %s ...\n", asset.Name)

	if err := update.DownloadAndInstall(asset.BrowserDownloadURL, archiveName); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nUpdated to %s\n", release.TagName)
	fmt.Fprintln(os.Stderr, "Run 'dolphin --version' to verify.")
	return nil
}

func listVersions() error {
	client := update.NewGitHubClient()
	releases, err := client.ListReleases(context.Background(), 20)
	if err != nil {
		return err
	}

	if len(releases) == 0 {
		fmt.Fprintln(os.Stderr, "No releases found.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Available versions (%s/%s):\n", runtime.GOOS, runtime.GOARCH)
	for _, r := range releases {
		mark := " "
		if r.Prerelease {
			mark = " ⚠"
		}
		fmt.Fprintf(os.Stderr, "  %s%s\n", r.TagName, mark)
	}
	fmt.Fprintln(os.Stderr, "\n⚠ = pre-release")
	return nil
}
