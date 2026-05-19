package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"dolphin/internal/i18n"
	"dolphin/internal/update"

	"github.com/spf13/cobra"
)

func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdUpdateUse),
		Short: i18n.TL(i18n.KeyCmdUpdateShort),
		Long:  i18n.TL(i18n.KeyCmdUpdateLong),
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUpdate,
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

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateCurrent)+"\n", Version))
	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdatePlatform)+"\n\n", runtime.GOOS, runtime.GOARCH))

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

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateRelease)+"\n", release.TagName))

	if Version == release.TagName {
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateAlreadyLatest)+"\n", release.TagName))
		return nil
	}

	asset, archiveName := update.FindAsset(release)
	if asset == nil {
		return fmt.Errorf("no release asset found for %s/%s (expected %q)", runtime.GOOS, runtime.GOARCH, archiveName)
	}

	if !force {
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateReady)+"\n", release.TagName, archiveName))
		fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateBinary)+"\n", update.MustExecPath()))
		fmt.Fprint(os.Stderr, i18n.TL(i18n.KeyUpdateConfirm))

		var input string
		_, _ = fmt.Scanln(&input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyUpdateCancelled))
			return nil
		}
	}

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateDownloading)+"\n", asset.Name))

	if err := update.DownloadAndInstall(asset.BrowserDownloadURL, archiveName); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateComplete)+"\n", release.TagName))
	fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyUpdateVerify))
	return nil
}

func listVersions() error {
	client := update.NewGitHubClient()
	releases, err := client.ListReleases(context.Background(), 20)
	if err != nil {
		return err
	}

	if len(releases) == 0 {
		fmt.Fprintln(os.Stderr, i18n.TL(i18n.KeyUpdateNoReleases))
		return nil
	}

	fmt.Fprint(os.Stderr, fmt.Sprintf(i18n.TL(i18n.KeyUpdateAvailable)+"\n", runtime.GOOS, runtime.GOARCH))
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
