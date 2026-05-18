package cmd

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"dolphin/internal/config"

	"github.com/spf13/cobra"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run self-diagnosis checks",
		Long: `Run self-diagnosis checks on the system to identify configuration issues.

Checks performed:
  - Config file locations and parseability
  - LLM API key presence and endpoint connectivity
  - Session directory accessibility
  - Transport configuration consistency
  - SSH host key availability
  - Skills and MCP directories
  - Port availability for enabled transports`,
		RunE: runDoctor,
	}
}

type checkResult struct {
	name   string
	status string // OK, WARN, FAIL
	detail string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	var results []checkResult

	// 1. Config file check
	results = append(results, checkConfigFiles()...)

	// 2. Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		results = append(results, checkResult{
			name: "config validation", status: "FAIL",
			detail: fmt.Sprintf("config.Load failed: %v", err),
		})
	} else {
		results = append(results, checkResult{name: "config validation", status: "OK", detail: "config loaded and validated"})

		// 3. LLM check
		results = append(results, checkLLM(cfg)...)

		// 4. Session directory
		results = append(results, checkSessionDir()...)

		// 5. Transport checks
		results = append(results, checkTransports(cfg)...)

		// 6. SSH host key
		results = append(results, checkSSHHostKey(cfg)...)

		// 7. Skills directory
		results = append(results, checkSkillsDir(cfg)...)

		// 8. MCP shell config
		results = append(results, checkMCPShell(cfg)...)

		// 9. Port availability
		results = append(results, checkPorts(cfg)...)
	}

	// Print results
	fmt.Println("Dolphin Doctor")
	fmt.Println("==============")
	fmt.Println()

	pass := 0
	fail := 0
	warn := 0

	for _, r := range results {
		switch r.status {
		case "OK":
			pass++
			fmt.Printf("  [OK]   %s: %s\n", r.name, r.detail)
		case "WARN":
			warn++
			fmt.Printf("  [WARN] %s: %s\n", r.name, r.detail)
		case "FAIL":
			fail++
			fmt.Printf("  [FAIL] %s: %s\n", r.name, r.detail)
		}
	}

	fmt.Println()
	fmt.Printf("Results: %d pass, %d warn, %d fail\n", pass, warn, fail)

	if fail > 0 {
		fmt.Println()
		fmt.Println("Run 'dolphin setup' to fix configuration issues.")
	}

	return nil
}

func checkConfigFiles() []checkResult {
	var results []checkResult

	sysDir := config.SystemConfigDir
	sysFile := filepath.Join(sysDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(sysFile); err == nil {
		results = append(results, checkResult{name: "system config", status: "OK", detail: sysFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: "system config", status: "WARN", detail: fmt.Sprintf("not found (%s), skipping", sysFile)})
	} else {
		results = append(results, checkResult{name: "system config", status: "FAIL", detail: fmt.Sprintf("unreadable: %v", err)})
	}

	home, _ := os.UserHomeDir()
	userFile := filepath.Join(home, config.UserConfigDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(userFile); err == nil {
		results = append(results, checkResult{name: "user config", status: "OK", detail: userFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: "user config", status: "WARN", detail: fmt.Sprintf("not found (%s), skipping", userFile)})
	} else {
		results = append(results, checkResult{name: "user config", status: "FAIL", detail: fmt.Sprintf("unreadable: %v", err)})
	}

	projFile := filepath.Join(config.ProjectConfigDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(projFile); err == nil {
		results = append(results, checkResult{name: "project config", status: "OK", detail: projFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: "project config", status: "WARN", detail: fmt.Sprintf("not found (%s), skipping", projFile)})
	} else {
		results = append(results, checkResult{name: "project config", status: "FAIL", detail: fmt.Sprintf("unreadable: %v", err)})
	}

	return results
}

func checkLLM(cfg *config.Config) []checkResult {
	var results []checkResult

	if !cfg.LLMConfigured() {
		results = append(results, checkResult{name: "LLM API key", status: "FAIL", detail: "no API key found — set DZ_LLM_API_KEY env var or run 'dolphin setup'"})
		return results
	}
	results = append(results, checkResult{name: "LLM API key", status: "OK", detail: "configured"})

	providers := cfg.LLM.EffectiveProviders()
	if len(providers) == 0 {
		results = append(results, checkResult{name: "LLM providers", status: "FAIL", detail: "no providers configured"})
		return results
	}

	for _, p := range providers {
		baseURL := p.BaseURL
		if baseURL == "" {
			baseURL = cfg.LLM.BaseURL
		}
		if baseURL == "" {
			results = append(results, checkResult{name: fmt.Sprintf("LLM %q base URL", p.Name), status: "FAIL", detail: "base URL is empty"})
			continue
		}

		client := &http.Client{Timeout: 5 * time.Second}
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		resp, err := client.Get(baseURL)
		if err != nil {
			results = append(results, checkResult{
				name:   fmt.Sprintf("LLM %q reachability", p.Name),
				status: "WARN",
				detail: fmt.Sprintf("%s unreachable: %v (check network or proxy)", baseURL, err),
			})
		} else {
			resp.Body.Close()
			results = append(results, checkResult{
				name:   fmt.Sprintf("LLM %q reachability", p.Name),
				status: "OK",
				detail: fmt.Sprintf("%s reachable", baseURL),
			})
		}
	}

	return results
}

func checkSessionDir() []checkResult {
	dir := config.SessionsDir()
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []checkResult{{name: "session directory", status: "WARN", detail: fmt.Sprintf("%s does not exist (will be created on first run)", dir)}}
		}
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf("%s: %v", dir, err)}}
	}
	if !info.IsDir() {
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf("%s is not a directory", dir)}}
	}
	f, err := os.CreateTemp(dir, ".doctor-write-test-*")
	if err != nil {
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf("%s is not writable: %v", dir, err)}}
	}
	_ = os.Remove(f.Name())
	f.Close()
	return []checkResult{{name: "session directory", status: "OK", detail: dir}}
}

func checkTransports(cfg *config.Config) []checkResult {
	var results []checkResult

	if cfg.Transport.Stdio.Enabled {
		results = append(results, checkResult{name: "transport stdio", status: "OK", detail: "enabled"})
	}
	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		results = append(results, checkResult{name: "transport ssh", status: "OK", detail: fmt.Sprintf("enabled on %s (user: %s)", addr, cfg.Transport.SSH.Username)})
		if cfg.Transport.SSH.Password == "" {
			results = append(results, checkResult{name: "SSH password", status: "FAIL", detail: "SSH password is empty — will be auto-generated, check logs"})
		}
	}
	if cfg.Transport.MQTT.Enabled {
		results = append(results, checkResult{name: "transport mqtt", status: "OK", detail: fmt.Sprintf("enabled (broker: %s)", cfg.Transport.MQTT.Broker)})
	}
	if cfg.Transport.Email.Enabled {
		results = append(results, checkResult{name: "transport email", status: "OK", detail: fmt.Sprintf("enabled (from: %s)", cfg.Transport.Email.From)})
	}

	anyEnabled := cfg.Transport.Stdio.Enabled || cfg.Transport.SSH.Enabled || cfg.Transport.MQTT.Enabled || cfg.Transport.Email.Enabled
	if !anyEnabled {
		results = append(results, checkResult{name: "transports", status: "FAIL", detail: "no transport enabled — enable at least one (stdio, ssh, mqtt, or email)"})
	}

	return results
}

func checkSSHHostKey(cfg *config.Config) []checkResult {
	if !cfg.Transport.SSH.Enabled {
		return nil
	}

	hostKey := cfg.Transport.SSH.HostKey
	if hostKey == "" {
		hostKey = "~/.ssh/id_ed25519"
	}

	if len(hostKey) > 0 && hostKey[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return []checkResult{{name: "SSH host key", status: "FAIL", detail: fmt.Sprintf("cannot expand ~: %v", err)}}
		}
		hostKey = filepath.Clean(home + hostKey[1:])
	}

	if _, err := os.Stat(hostKey); err != nil {
		home, _ := os.UserHomeDir()
		autoKey := filepath.Join(home, ".dolphin", "ssh_host_key")
		if _, err2 := os.Stat(autoKey); err2 != nil {
			return []checkResult{{name: "SSH host key", status: "WARN", detail: fmt.Sprintf("no host key at %s or %s — will auto-generate ephemeral key", hostKey, autoKey)}}
		}
		return []checkResult{{name: "SSH host key", status: "OK", detail: fmt.Sprintf("auto-generated key at %s", autoKey)}}
	}
	return []checkResult{{name: "SSH host key", status: "OK", detail: hostKey}}
}

func checkSkillsDir(cfg *config.Config) []checkResult {
	dir := cfg.Skills.Dir
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []checkResult{{name: "skills directory", status: "WARN", detail: fmt.Sprintf("%s does not exist (will be created on first run)", dir)}}
		}
		return []checkResult{{name: "skills directory", status: "FAIL", detail: fmt.Sprintf("%s: %v", dir, err)}}
	}
	if !info.IsDir() {
		return []checkResult{{name: "skills directory", status: "FAIL", detail: fmt.Sprintf("%s is not a directory", dir)}}
	}
	return []checkResult{{name: "skills directory", status: "OK", detail: dir}}
}

func checkMCPShell(cfg *config.Config) []checkResult {
	if !cfg.MCP.Shell.Enabled {
		return []checkResult{{name: "MCP shell", status: "WARN", detail: "shell tool is disabled (mcp.shell.enabled=false)"}}
	}
	if cfg.MCP.Shell.AllowUnrestricted && len(cfg.MCP.Shell.AllowedCommands) == 0 {
		return []checkResult{{name: "MCP shell", status: "WARN", detail: "unrestricted mode — any shell command is allowed"}}
	}
	if len(cfg.MCP.Shell.AllowedCommands) > 0 {
		return []checkResult{{name: "MCP shell", status: "OK", detail: fmt.Sprintf("restricted to: %v", cfg.MCP.Shell.AllowedCommands)}}
	}
	return []checkResult{{name: "MCP shell", status: "OK", detail: "enabled with default restrictions"}}
}

func checkPorts(cfg *config.Config) []checkResult {
	var results []checkResult

	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		results = append(results, checkPort(addr, "SSH")...)
	}
	if cfg.Health.Enabled && cfg.Health.Addr != "" {
		results = append(results, checkPort(cfg.Health.Addr, "health")...)
	}
	if cfg.Metrics.Enabled && cfg.Metrics.Addr != "" {
		results = append(results, checkPort(cfg.Metrics.Addr, "metrics")...)
	}
	if cfg.Transport.MQTT.Embedded && cfg.Transport.MQTT.EmbeddedAddr != "" {
		results = append(results, checkPort(cfg.Transport.MQTT.EmbeddedAddr, "MQTT broker")...)
	}
	if cfg.Pprof.Enabled && cfg.Pprof.Addr != "" {
		results = append(results, checkPort(cfg.Pprof.Addr, "pprof")...)
	}

	return results
}

func checkPort(addr, label string) []checkResult {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return []checkResult{{
			name:   fmt.Sprintf("port %s", label),
			status: "WARN",
			detail: fmt.Sprintf("%s in use or unavailable (%v)", addr, err),
		}}
	}
	_ = listener.Close()
	return []checkResult{{
		name:   fmt.Sprintf("port %s", label),
		status: "OK",
		detail: fmt.Sprintf("%s available", addr),
	}}
}
