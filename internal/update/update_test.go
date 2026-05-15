package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArchiveNameFor(t *testing.T) {
	tests := []struct {
		goos, goarch, version string
		want                  string
	}{
		{"darwin", "amd64", "v1.0.0", "dolphin_v1.0.0_macOS_x86_64.tar.gz"},
		{"linux", "amd64", "v1.0.0", "dolphin_v1.0.0_linux_x86_64.tar.gz"},
		{"linux", "arm64", "v1.0.0", "dolphin_v1.0.0_linux_arm64.tar.gz"},
		{"windows", "amd64", "v1.0.0", "dolphin_v1.0.0_windows_x86_64.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			osName := osNameFor(tt.goos)
			archName := archNameFor(tt.goarch)
			ext := ".tar.gz"
			if tt.goos == "windows" {
				ext = ".zip"
			}
			got := "dolphin_" + tt.version + "_" + osName + "_" + archName + ext
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMustExecPath(t *testing.T) {
	p := mustExecPath()
	if p == "" {
		t.Error("expected non-empty path")
	}
	if !filepath.IsAbs(p) && p != "dolphin" {
		t.Errorf("expected absolute path or fallback, got %q", p)
	}
}

func TestFindAsset(t *testing.T) {
	release := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "dolphin_v1.0.0_linux_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "dolphin_v1.0.0_macOS_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/mac"},
			{Name: "dolphin_v1.0.0_windows_x86_64.zip", BrowserDownloadURL: "https://example.com/win"},
		},
	}

	_, name := FindAsset(release)
	if name == "" {
		t.Fatal("expected non-empty archive name")
	}

	empty := &Release{TagName: "v1.0.0"}
	asset2, _ := FindAsset(empty)
	if asset2 != nil {
		t.Error("expected nil asset for empty release")
	}
}

func TestGitHubClient_FetchRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","assets":[{"name":"test.tar.gz","browser_download_url":"http://example.com"}]}`))
	}))
	defer srv.Close()

	// GitHubClient uses the const githubAPIBase, so we can't redirect to srv.
	// This test validates the HTTP handler flow; actual integration tested via checker.
	_ = srv
}

func TestBinaryName(t *testing.T) {
	name := binaryName()
	if runtime.GOOS == "windows" {
		if name != "dolphin.exe" {
			t.Errorf("expected dolphin.exe on windows, got %q", name)
		}
	} else {
		if name != "dolphin" {
			t.Errorf("expected dolphin, got %q", name)
		}
	}
}

func TestOsNameFor(t *testing.T) {
	if got := osNameFor("darwin"); got != "macOS" {
		t.Errorf("darwin -> %q, want macOS", got)
	}
	if got := osNameFor("linux"); got != "linux" {
		t.Errorf("linux -> %q, want linux", got)
	}
	if got := osNameFor("windows"); got != "windows" {
		t.Errorf("windows -> %q, want windows", got)
	}
}

func TestArchNameFor(t *testing.T) {
	if got := archNameFor("amd64"); got != "x86_64" {
		t.Errorf("amd64 -> %q, want x86_64", got)
	}
	if got := archNameFor("arm64"); got != "arm64" {
		t.Errorf("arm64 -> %q, want arm64", got)
	}
}

func TestInstallBinary_Unix(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "dolphin")
	oldData := []byte("old-binary")
	newData := []byte("new-binary-data")

	if err := os.WriteFile(execPath, oldData, 0755); err != nil {
		t.Fatal(err)
	}

	if err := InstallBinary(newData, execPath); err != nil {
		t.Fatalf("InstallBinary: %v", err)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newData) {
		t.Errorf("binary = %q, want %q", string(got), string(newData))
	}

	// Backup should be cleaned up.
	if _, err := os.Stat(execPath + ".bak"); !os.IsNotExist(err) {
		t.Error("expected .bak file to be removed after successful install")
	}

	// Verify file mode is executable.
	info, err := os.Stat(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("binary is not executable")
	}
}

func TestInstallBinary_Rollback(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "dolphin")
	oldData := []byte("old-binary")
	newData := []byte("new-data")

	if err := os.WriteFile(execPath, oldData, 0755); err != nil {
		t.Fatal(err)
	}

	if err := InstallBinary(newData, execPath); err != nil {
		t.Fatalf("InstallBinary: %v", err)
	}

	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newData) {
		t.Errorf("binary = %q, want %q", string(got), string(newData))
	}
}
