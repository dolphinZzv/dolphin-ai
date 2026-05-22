package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/h2non/gock"
)

func TestNewGitHubClient(t *testing.T) {
	client := NewGitHubClient()
	if client.HTTPClient == nil {
		t.Error("expected HTTPClient to be initialized")
	}
	if client.HTTPClient.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", client.HTTPClient.Timeout)
	}
}

func TestFetchLatest_Stable(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/latest").
		Reply(200).
		JSON(Release{
			TagName: "v1.2.3",
			Assets: []Asset{{
				Name:               "dolphin_v1.2.3_linux_x86_64.tar.gz",
				BrowserDownloadURL: "https://example.com/dl",
			}},
		})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	release, err := client.FetchLatest(context.Background(), "stable")
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Errorf("got tag %q, want v1.2.3", release.TagName)
	}
}

func TestFetchLatest_PreRelease(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases").
		MatchParam("per_page", "5").
		Reply(200).
		JSON([]ReleaseSummary{
			{TagName: "v2.0.0-beta.1", Prerelease: true},
			{TagName: "v1.0.0", Prerelease: false},
		})
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/tags/v2.0.0-beta.1").
		Reply(200).
		JSON(Release{TagName: "v2.0.0-beta.1", Assets: []Asset{{
			Name:               "dolphin_v2.0.0-beta.1_linux_x86_64.tar.gz",
			BrowserDownloadURL: "https://example.com/dl",
		}}})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	release, err := client.FetchLatest(context.Background(), "pre-release")
	if err != nil {
		t.Fatalf("FetchLatest pre-release: %v", err)
	}
	if release.TagName != "v2.0.0-beta.1" {
		t.Errorf("got tag %q, want v2.0.0-beta.1", release.TagName)
	}
}

func TestFetchLatest_NoReleases(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases").
		MatchParam("per_page", "5").
		Reply(200).
		JSON([]ReleaseSummary{})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	_, err := client.FetchLatest(context.Background(), "pre-release")
	if err == nil {
		t.Fatal("expected error for no releases")
	}
	if !strings.Contains(err.Error(), "no releases found") {
		t.Errorf("expected 'no releases found' error, got: %v", err)
	}
}

func TestFetchRelease_ByTag(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/tags/v1.0.0").
		Reply(200).
		JSON(Release{TagName: "v1.0.0", Assets: []Asset{{
			Name:               "dolphin_v1.0.0_linux_x86_64.tar.gz",
			BrowserDownloadURL: "https://example.com/dl",
		}}})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	release, err := client.FetchRelease(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("FetchRelease: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Errorf("got tag %q, want v1.0.0", release.TagName)
	}
}

func TestFetchRelease_NotFound(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/tags/v0.0.0").
		Reply(404).
		BodyString("not found")

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	_, err := client.FetchRelease(context.Background(), "v0.0.0")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestFetchRelease_EmptyTagName(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/latest").
		Reply(200).
		JSON(Release{TagName: ""})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	_, err := client.FetchLatest(context.Background(), "stable")
	if err == nil {
		t.Fatal("expected error for empty tag name")
	}
}

func TestFetchRelease_InvalidJSON(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/latest").
		Reply(200).
		BodyString("{invalid")

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	_, err := client.FetchLatest(context.Background(), "stable")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchRelease_WithToken(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases/latest").
		MatchHeader("Authorization", "Bearer test-token").
		Reply(200).
		JSON(Release{TagName: "v1.0.0"})

	client := &GitHubClient{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Token:      "test-token",
	}
	gock.InterceptClient(client.HTTPClient)
	release, err := client.FetchLatest(context.Background(), "stable")
	if err != nil {
		t.Fatalf("FetchLatest with token: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Errorf("got tag %q, want v1.0.0", release.TagName)
	}
}

func TestFetchLatest_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewGitHubClient()
	_, err := client.FetchLatest(ctx, "stable")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestListReleases(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases").
		MatchParam("per_page", "5").
		Reply(200).
		JSON([]ReleaseSummary{
			{TagName: "v2.0.0", Prerelease: false},
			{TagName: "v1.0.0", Prerelease: false},
		})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	releases, err := client.ListReleases(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	if len(releases) != 2 {
		t.Errorf("expected 2 releases, got %d", len(releases))
	}
	if releases[0].TagName != "v2.0.0" {
		t.Errorf("got %q, want v2.0.0", releases[0].TagName)
	}
}

func TestListReleases_Empty(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases").
		MatchParam("per_page", "3").
		Reply(200).
		JSON([]ReleaseSummary{})

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	releases, err := client.ListReleases(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("expected 0 releases, got %d", len(releases))
	}
}

func TestListReleases_ServerError(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").
		Get("/repos/dolphinZzv/dolphin/releases").
		Reply(500).
		BodyString("internal server error")

	client := NewGitHubClient()
	gock.InterceptClient(client.HTTPClient)
	_, err := client.ListReleases(context.Background(), 5)
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestDownloadAndInstall_HTTPError(t *testing.T) {
	defer gock.Off()
	gock.New("https://example.com").
		Get("/download/test.tar.gz").
		Reply(500)

	err := DownloadAndInstall("https://example.com/download/test.tar.gz", "test.tar.gz")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestExtractBinary_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.tar.gz")
	binContent := []byte("#!/bin/sh\necho hello\n")
	if err := createTarGz(archivePath, binaryName(), binContent); err != nil {
		t.Fatal(err)
	}

	data, err := extractBinary(archivePath)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if string(data) != string(binContent) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(binContent))
	}
}

func TestExtractBinary_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.tar.gz")
	if err := createTarGz(archivePath, "other-binary", []byte("data")); err != nil {
		t.Fatal(err)
	}

	_, err := extractBinary(archivePath)
	if err == nil {
		t.Fatal("expected error for missing binary in archive")
	}
}

func TestExtractBinary_InvalidPath(t *testing.T) {
	_, err := extractBinary("/nonexistent/path/to/archive.tar.gz")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestExtractBinaryFromTarGz_InvalidGzip(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "bad.tar.gz")
	if err := os.WriteFile(invalidPath, []byte("not a gzip"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := extractBinaryFromTarGz(invalidPath)
	if err == nil {
		t.Fatal("expected error for invalid gzip")
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "archive.zip")
	if err := createZipWithBinary(zipPath, binaryName(), []byte("binary-content")); err != nil {
		t.Fatalf("create zip: %v", err)
	}

	data, err := extractBinaryFromZip(zipPath)
	if err != nil {
		t.Fatalf("extractBinaryFromZip: %v", err)
	}
	if string(data) != "binary-content" {
		t.Errorf("got %q, want %q", string(data), "binary-content")
	}
}

func TestExtractBinaryFromZip_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "archive.zip")
	if err := createZipWithBinary(zipPath, "other-binary", []byte("data")); err != nil {
		t.Fatalf("create zip: %v", err)
	}

	_, err := extractBinaryFromZip(zipPath)
	if err == nil {
		t.Fatal("expected error for missing binary in zip")
	}
}

func TestInstallBinary_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "dolphin")

	if err := os.WriteFile(execPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := InstallBinary([]byte("new"), execPath); err != nil {
		t.Fatalf("InstallBinary: %v", err)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("got %q, want %q", string(got), "new")
	}
}

func TestInstallBinary_NoExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "dolphin")

	// On Unix, InstallBinary tries to rename the current binary first.
	// If it doesn't exist, the rename backup step fails.
	err := InstallBinary([]byte("new"), execPath)
	if err == nil {
		t.Logf("InstallBinary succeeded without existing binary (acceptable)")
	}
}

func TestApplyStagedUpdate_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	if ApplyStagedUpdate("/some/path") {
		t.Error("expected ApplyStagedUpdate to return false on Unix")
	}
}

func TestMustExecPath(t *testing.T) {
	p := MustExecPath()
	if p == "" {
		t.Error("MustExecPath should return non-empty path")
	}
}

func TestArchiveExt(t *testing.T) {
	ext := archiveExt()
	if runtime.GOOS == "windows" && ext != ".zip" {
		t.Errorf("expected .zip on windows, got %q", ext)
	}
	if runtime.GOOS != "windows" && ext != ".tar.gz" {
		t.Errorf("expected .tar.gz, got %q", ext)
	}
}

func TestOsNameFor(t *testing.T) {
	for _, tt := range []struct{ goos, want string }{
		{"darwin", "macOS"},
		{"linux", "linux"},
		{"windows", "windows"},
		{"freebsd", "freebsd"},
	} {
		got := osNameFor(tt.goos)
		if got != tt.want {
			t.Errorf("osNameFor(%q) = %q, want %q", tt.goos, got, tt.want)
		}
	}
}

func TestArchNameFor(t *testing.T) {
	for _, tt := range []struct{ goarch, want string }{
		{"amd64", "x86_64"},
		{"arm64", "arm64"},
		{"riscv64", "riscv64"},
	} {
		got := archNameFor(tt.goarch)
		if got != tt.want {
			t.Errorf("archNameFor(%q) = %q, want %q", tt.goarch, got, tt.want)
		}
	}
}

func TestBinaryName(t *testing.T) {
	name := binaryName()
	if runtime.GOOS == "windows" && name != "dolphin.exe" {
		t.Errorf("expected dolphin.exe, got %q", name)
	}
	if runtime.GOOS != "windows" && name != "dolphin" {
		t.Errorf("expected dolphin, got %q", name)
	}
}

func TestFindAsset_Matching(t *testing.T) {
	release := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "dolphin_v1.0.0_linux_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "dolphin_v1.0.0_macOS_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/mac"},
			{Name: "dolphin_v1.0.0_windows_x86_64.zip", BrowserDownloadURL: "https://example.com/win"},
		},
	}

	asset, name := FindAsset(release)
	if asset == nil {
		t.Fatal("expected to find matching asset")
	}
	if name == "" {
		t.Error("expected non-empty archive name")
	}
}

func TestFindAsset_NoMatch(t *testing.T) {
	release := &Release{TagName: "v99.99.99"}
	asset, name := FindAsset(release)
	if asset != nil {
		t.Error("expected nil asset for no matching platform")
	}
	if name != "" {
		t.Errorf("expected empty archive name, got %q", name)
	}
}

func createTarGz(dst, name string, content []byte) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	hdr := &tar.Header{
		Name: name,
		Mode: 0755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(content); err != nil {
		return err
	}
	return nil
}

func createZipWithBinary(dst, name string, content []byte) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	out, err := w.Create(name)
	if err != nil {
		return err
	}
	_, err = out.Write(content)
	return err
}
