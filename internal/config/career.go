package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"dolphin/internal/i18n"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

// FirstRunMarker returns the path to the first-run marker file.
func FirstRunMarker() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(hd, UserConfigDir, "first-run")
}

// IsFirstRun checks whether this is the application's first run.
// Returns true when the marker file does NOT exist.
func IsFirstRun() bool {
	_, err := os.Stat(FirstRunMarker())
	return os.IsNotExist(err)
}

// MarkFirstRunDone removes the first-run marker.
func MarkFirstRunDone() error {
	return os.Remove(FirstRunMarker())
}

// CreateFirstRunMarker creates the first-run marker file.
func CreateFirstRunMarker() error {
	path := FirstRunMarker()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0600)
}

// DolphinIDFile returns the path to the dolphin instance ID file.
func DolphinIDFile() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(hd, UserConfigDir, "id")
}

// LoadOrCreateDolphinID reads the persisted instance ID, or generates a new one
// via xid and persists it. Returns empty string only when home dir is unavailable.
func LoadOrCreateDolphinID() string {
	path := DolphinIDFile()
	if path == "" {
		return xid.New().String()
	}
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		return string(data)
	}
	id := xid.New().String()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return id
	}
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		zap.S().Warnw("failed to persist dolphin id", "error", err)
	}
	return id
}

// EmailConfiguredMarker returns the path to the email-configured marker file.
func EmailConfiguredMarker() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(hd, UserConfigDir, "email-configured")
}

// IsEmailConfigured checks whether the email transport has already been
// configured and sent its initial startup notification.
func IsEmailConfigured() bool {
	_, err := os.Stat(EmailConfiguredMarker())
	return err == nil
}

// MarkEmailConfigured creates the email-configured marker so future
// startups skip the welcome email.
func MarkEmailConfigured() error {
	path := EmailConfiguredMarker()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0600)
}

// GenerateSystemMD creates SYSTEM.md with system environment info.
// Content adapts to the user's detected language.
func shellName() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		for _, s := range []string{"pwsh.exe", "powershell.exe", "cmd.exe", "bash.exe"} {
			if _, err := os.Stat(filepath.Join(os.Getenv("SystemRoot"), "System32", s)); err == nil {
				return s
			}
		}
	}
	return "unknown"
}

func GenerateSystemMD(lang i18n.Lang) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	path := filepath.Join(home, UserConfigDir, "SYSTEM.md")

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(i18n.T(i18n.KeySystemMDContent, lang))
	sb.WriteString("\n\n")

	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()

	switch lang {
	case i18n.ZH:
		fmt.Fprintf(&sb, "- 操作系统: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(&sb, "- 主机名: %s\n", hostname)
		fmt.Fprintf(&sb, "- Shell: %s\n", shellName())
		fmt.Fprintf(&sb, "- 用户目录: %s\n", home)
		fmt.Fprintf(&sb, "- 工作目录: %s\n", cwd)
		fmt.Fprintf(&sb, "- CPU 核心数: %d\n", runtime.NumCPU())
	default:
		fmt.Fprintf(&sb, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(&sb, "- Hostname: %s\n", hostname)
		fmt.Fprintf(&sb, "- Shell: %s\n", shellName())
		fmt.Fprintf(&sb, "- Home: %s\n", home)
		fmt.Fprintf(&sb, "- Working Dir: %s\n", cwd)
		fmt.Fprintf(&sb, "- CPUs: %d\n", runtime.NumCPU())
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(sb.String()), 0600)
}

// PromptSystemMD asks the user whether to auto-generate SYSTEM.md with system info.
// Returns true if generated.
func PromptSystemMD() (bool, error) {
	lang := i18n.DetectLang()
	fmt.Fprintf(os.Stderr, "\n%s\n", i18n.T(i18n.KeySystemMDPrompt, lang))
	fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeySystemMDExplain, lang))
	fmt.Fprintf(os.Stderr, "  [y] %s  [n] %s\n", i18n.T(i18n.KeySystemMDYes, lang), i18n.T(i18n.KeySystemMDNo, lang))
	fmt.Fprintf(os.Stderr, "%s: ", i18n.T(i18n.KeyChoice, lang))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, nil //nolint:nilerr
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "y" && input != "yes" {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeySystemMDSkipped, lang))
		return false, nil
	}

	path, err := GenerateSystemMD(lang)
	if err != nil {
		return false, fmt.Errorf("generate SYSTEM.md: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.T(i18n.KeySystemMDGenerated, lang), path)
	return true, nil
}
