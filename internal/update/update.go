package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubAPIBase = "https://api.github.com"
	repoOwner     = "dolphinZzv"
	repoName      = "dolphin"
)

// Release represents a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a downloadable file in a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ReleaseSummary is a lightweight view of a release for listing.
type ReleaseSummary struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
}

// GitHubClient handles communication with the GitHub Releases API.
type GitHubClient struct {
	HTTPClient *http.Client
	Token      string
}

// NewGitHubClient creates a client that reads GITHUB_TOKEN from the environment.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Token:      os.Getenv("GITHUB_TOKEN"),
	}
}

// FetchLatest fetches the latest release for the given channel.
// "stable" uses /releases/latest (GitHub excludes pre-releases and drafts).
// "pre-release" fetches the most recent release regardless of prerelease status.
func (c *GitHubClient) FetchLatest(ctx context.Context, channel string) (*Release, error) {
	if channel == "pre-release" {
		releases, err := c.listReleases(ctx, 5)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			return nil, fmt.Errorf("no releases found")
		}
		return c.FetchRelease(ctx, releases[0].TagName)
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, repoOwner, repoName)
	return c.fetchRelease(ctx, url)
}

// FetchRelease fetches a specific release by tag name.
func (c *GitHubClient) FetchRelease(ctx context.Context, tag string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, repoOwner, repoName, tag)
	return c.fetchRelease(ctx, url)
}

func (c *GitHubClient) fetchRelease(ctx context.Context, url string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("release not found")
	}
	return &release, nil
}

// ListReleases lists recent releases.
func (c *GitHubClient) ListReleases(ctx context.Context, perPage int) ([]ReleaseSummary, error) {
	return c.listReleases(ctx, perPage)
}

func (c *GitHubClient) listReleases(ctx context.Context, perPage int) ([]ReleaseSummary, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", githubAPIBase, repoOwner, repoName, perPage)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var releases []ReleaseSummary
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// FindAsset finds the download asset matching the current OS/arch.
// Returns the asset and the expected archive filename, or nil/"" if not found.
func FindAsset(release *Release) (*Asset, string) {
	archiveName := archiveNameFor(release.TagName)
	for i := range release.Assets {
		if release.Assets[i].Name == archiveName {
			return &release.Assets[i], archiveName
		}
	}
	return nil, ""
}

// DownloadAndInstall downloads a release archive, extracts the binary,
// and installs it to replace the current executable.
func DownloadAndInstall(assetURL, archiveName string) error {
	tmpFile, err := os.CreateTemp("", "dolphin-*"+archiveExt())
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := http.Get(assetURL)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	binaryData, err := extractBinary(tmpPath)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	execPath := mustExecPath()
	return InstallBinary(binaryData, execPath)
}

// MustExecPath returns the absolute path to the current running executable.
func MustExecPath() string {
	return mustExecPath()
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
			return io.ReadAll(tarReader)
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
