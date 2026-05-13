package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	githubAPIBase = "https://api.github.com"
	repoOwner     = "dolphinZzv"
	repoName      = "dolphin"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

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

	// Fetch release from GitHub
	release, err := fetchRelease(version)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Release: %s\n", release.TagName)

	archiveName := archiveNameFor(release.TagName)

	var asset *githubAsset
	for _, a := range release.Assets {
		if a.Name == archiveName {
			asset = &a
			break
		}
	}
	if asset == nil {
		return fmt.Errorf("no release asset found for %s/%s (expected %q)", runtime.GOOS, runtime.GOARCH, archiveName)
	}

	// Confirm download
	if Version == release.TagName {
		fmt.Fprintf(os.Stderr, "Already at version %s. No update needed.\n", release.TagName)
		return nil
	}

	if !force {
		fmt.Fprintf(os.Stderr, "\nReady to download and install %s (%s)\n", release.TagName, archiveName)
		fmt.Fprintf(os.Stderr, "Current binary: %s\n", mustExecPath())
		fmt.Fprintf(os.Stderr, "Are you sure? [y/N]: ")

		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Update cancelled.")
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "\nDownloading %s ...\n", asset.Name)

	tmpFile, err := os.CreateTemp("", "dolphin-*"+archiveExt())
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := downloadFile(asset.BrowserDownloadURL, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	fmt.Fprintf(os.Stderr, "Extracting ...\n")

	binaryData, err := extractBinary(tmpPath)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	execPath := mustExecPath()

	fmt.Fprintf(os.Stderr, "Installing to %s ...\n", execPath)

	tmpBin := execPath + ".dolphin-tmp"
	if err := os.WriteFile(tmpBin, binaryData, 0755); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}

	backupPath := execPath + ".bak"
	os.Remove(backupPath)
	os.Rename(execPath, backupPath)

	if err := os.Rename(tmpBin, execPath); err != nil {
		os.Rename(backupPath, execPath)
		os.Remove(tmpBin)
		return fmt.Errorf("install new binary: %w", err)
	}

	os.Remove(backupPath)

	fmt.Fprintf(os.Stderr, "\n✓ Updated to %s\n", release.TagName)
	fmt.Fprintln(os.Stderr, "Run 'dolphin --version' to verify.")
	return nil
}

func mustExecPath() string {
	p, err := os.Executable()
	if err != nil {
		return "dolphin"
	}
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return real
	}
	return p
}

func listVersions() error {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=20", githubAPIBase, repoOwner, repoName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var releases []struct {
		TagName string `json:"tag_name"`
		Prerelease bool `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
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

func fetchRelease(version string) (*githubRelease, error) {
	var url string
	if version == "" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, repoOwner, repoName)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, repoOwner, repoName, version)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("release not found")
	}
	return &release, nil
}

func archiveNameFor(version string) string {
	osName := osNameFor(runtime.GOOS)
	archName := archNameFor(runtime.GOARCH)
	return fmt.Sprintf("dolphin_%s_%s_%s%s", version, osName, archName, archiveExt())
}

func archiveExt() string {
	if runtime.GOOS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

func osNameFor(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "windows"
	default:
		return goos
	}
}

func archNameFor(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	default:
		return goarch
	}
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "dolphin.exe"
	}
	return "dolphin"
}

func downloadFile(url string, w io.Writer) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

func extractBinary(archivePath string) ([]byte, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractBinaryFromZip(archivePath)
	}
	return extractBinaryFromTarGz(archivePath)
}

func extractBinaryFromTarGz(archivePath string) ([]byte, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	binName := binaryName()
	tarReader := tar.NewReader(gzr)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == binName {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("read %s from archive: %w", header.Name, err)
			}
			return data, nil
		}
	}

	return nil, fmt.Errorf("%s not found in archive", binName)
}

func extractBinaryFromZip(archivePath string) ([]byte, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	binName := binaryName()
	for _, f := range r.File {
		if filepath.Base(f.Name) == binName {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s in zip: %w", f.Name, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("read %s from zip: %w", f.Name, err)
			}
			return data, nil
		}
	}

	return nil, fmt.Errorf("%s not found in archive", binName)
}
