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
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdDoctorUse),
		Short: i18n.TL(i18n.KeyCmdDoctorShort),
		Long:  i18n.TL(i18n.KeyCmdDoctorLong),
		RunE:  runDoctor,
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
			name: i18n.TL(i18n.KeyDoctorCheckNameCfgVal), status: "FAIL",
			detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorCfgFail), err),
		})
	} else {
		results = append(results, checkResult{name: "config validation", status: "OK", detail: i18n.TL(i18n.KeyDoctorCfgValid)})

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
	fmt.Println(i18n.TL(i18n.KeyDoctorBanner))
	fmt.Println(i18n.TL(i18n.KeyDoctorSep))
	fmt.Println()

	pass := 0
	fail := 0
	warn := 0

	for _, r := range results {
		switch r.status {
		case "OK":
			pass++
			fmt.Printf(i18n.TL(i18n.KeyDoctorOK)+"\n", r.name, r.detail)
		case "WARN":
			warn++
			fmt.Printf(i18n.TL(i18n.KeyDoctorWarn)+"\n", r.name, r.detail)
		case "FAIL":
			fail++
			fmt.Printf(i18n.TL(i18n.KeyDoctorFail)+"\n", r.name, r.detail)
		}
	}

	fmt.Println()
	fmt.Printf(i18n.TL(i18n.KeyDoctorResults)+"\n", pass, warn, fail)

	if fail > 0 {
		fmt.Println()
		fmt.Println(i18n.TL(i18n.KeyDoctorFixHint))
	}

	return nil
}

func checkConfigFiles() []checkResult {
	var results []checkResult

	sysDir := config.SystemConfigDir
	sysFile := filepath.Join(sysDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(sysFile); err == nil {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgSys), status: "OK", detail: sysFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgSys), status: "WARN", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorCfgNotFound), sysFile)})
	} else {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgSys), status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorUnreadable), err)})
	}

	home, _ := os.UserHomeDir()
	userFile := filepath.Join(home, config.UserConfigDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(userFile); err == nil {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgUser), status: "OK", detail: userFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgUser), status: "WARN", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorCfgNotFound), userFile)})
	} else {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgUser), status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorUnreadable), err)})
	}

	projFile := filepath.Join(config.ProjectConfigDir, config.ConfigFileName+".yaml")
	if _, err := os.Stat(projFile); err == nil {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgProj), status: "OK", detail: projFile})
	} else if os.IsNotExist(err) {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgProj), status: "WARN", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorCfgNotFound), projFile)})
	} else {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameCfgProj), status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorUnreadable), err)})
	}

	return results
}

func checkLLM(cfg *config.Config) []checkResult {
	var results []checkResult

	if !cfg.LLMConfigured() {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameLLMKey), status: "FAIL", detail: "no API key found — set DZ_LLM_API_KEY env var or run 'dolphin setup'"})
		return results
	}
	results = append(results, checkResult{name: "LLM API key", status: "OK", detail: i18n.TL(i18n.KeyDoctorLLMKeyOK)})

	providers := cfg.LLM.EffectiveProviders()
	if len(providers) == 0 {
		results = append(results, checkResult{name: "LLM providers", status: "FAIL", detail: i18n.TL(i18n.KeyDoctorLLMProvNone)})
		return results
	}

	for _, p := range providers {
		baseURL := p.BaseURL
		if baseURL == "" {
			baseURL = cfg.LLM.BaseURL
		}
		if baseURL == "" {
			results = append(results, checkResult{name: fmt.Sprintf("LLM %q base URL", p.Name), status: "FAIL", detail: i18n.TL(i18n.KeyDoctorLLMBaseEmpty)})
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
				detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorLLMUnreachable), baseURL, err),
			})
		} else {
			resp.Body.Close()
			results = append(results, checkResult{
				name:   fmt.Sprintf("LLM %q reachability", p.Name),
				status: "OK",
				detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorLLMReachable), baseURL),
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
			return []checkResult{{name: i18n.TL(i18n.KeyDoctorCheckNameSessDir), status: "WARN", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessNotExist), dir)}}
		}
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessFail), dir, err)}}
	}
	if !info.IsDir() {
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessNotDir), dir)}}
	}
	f, err := os.CreateTemp(dir, ".doctor-write-test-*")
	if err != nil {
		return []checkResult{{name: "session directory", status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessNotWritable), dir, err)}}
	}
	_ = os.Remove(f.Name())
	f.Close()
	return []checkResult{{name: "session directory", status: "OK", detail: dir}}
}

func checkTransports(cfg *config.Config) []checkResult {
	var results []checkResult

	if cfg.Transport.Stdio.Enabled {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameTransStdio), status: "OK", detail: i18n.TL(i18n.KeyEnabled)})
	}
	if cfg.Transport.SSH.Enabled {
		addr := cfg.Transport.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameTransSSH), status: "OK", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorTransSSH), addr, cfg.Transport.SSH.Username)})
		if cfg.Transport.SSH.Password == "" {
			results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameSSHPass), status: "FAIL", detail: "SSH password is empty — will be auto-generated, check logs"})
		}
	}
	if cfg.Transport.MQTT.Enabled {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameTransMQTT), status: "OK", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorTransMQTT), cfg.Transport.MQTT.Broker)})
	}
	if cfg.Transport.Email.Enabled {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameTransEmail), status: "OK", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorTransEmail), cfg.Transport.Email.From)})
	}

	anyEnabled := cfg.Transport.Stdio.Enabled || cfg.Transport.SSH.Enabled || cfg.Transport.MQTT.Enabled || cfg.Transport.Email.Enabled
	if !anyEnabled {
		results = append(results, checkResult{name: i18n.TL(i18n.KeyDoctorCheckNameTransports), status: "FAIL", detail: "no transport enabled — enable at least one (stdio, ssh, mqtt, or email)"})
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
			return []checkResult{{name: i18n.TL(i18n.KeyDoctorCheckNameSSHKey), status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSSHKeyFail), err)}}
		}
		hostKey = filepath.Clean(home + hostKey[1:])
	}

	if _, err := os.Stat(hostKey); err != nil {
		home, _ := os.UserHomeDir()
		autoKey := filepath.Join(home, ".dolphin", "ssh_host_key")
		if _, err2 := os.Stat(autoKey); err2 != nil {
			return []checkResult{{name: "SSH host key", status: "WARN", detail: fmt.Sprintf("no host key at %s or %s — will auto-generate ephemeral key", hostKey, autoKey)}}
		}
		return []checkResult{{name: "SSH host key", status: "OK", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSSHKeyAuto), autoKey)}}
	}
	return []checkResult{{name: "SSH host key", status: "OK", detail: hostKey}}
}

func checkSkillsDir(cfg *config.Config) []checkResult {
	dir := cfg.Skills.Dir
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []checkResult{{name: i18n.TL(i18n.KeyDoctorCheckNameSkillsDir), status: "WARN", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessNotExist), dir)}}
		}
		return []checkResult{{name: "skills directory", status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessFail), dir, err)}}
	}
	if !info.IsDir() {
		return []checkResult{{name: "skills directory", status: "FAIL", detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorSessNotDir), dir)}}
	}
	return []checkResult{{name: "skills directory", status: "OK", detail: dir}}
}

func checkMCPShell(cfg *config.Config) []checkResult {
	if !cfg.MCP.Shell.Enabled {
		return []checkResult{{name: i18n.TL(i18n.KeyDoctorCheckNameShell), status: "WARN", detail: i18n.TL(i18n.KeyDoctorShellDisabled)}}
	}
	if cfg.MCP.Shell.AllowUnrestricted && len(cfg.MCP.Shell.AllowedCommands) == 0 {
		return []checkResult{{name: "MCP shell", status: "WARN", detail: "unrestricted mode — any shell command is allowed"}}
	}
	if len(cfg.MCP.Shell.AllowedCommands) > 0 {
		return []checkResult{{name: "MCP shell", status: "OK", detail: fmt.Sprintf("restricted to: %v", cfg.MCP.Shell.AllowedCommands)}}
	}
	return []checkResult{{name: "MCP shell", status: "OK", detail: i18n.TL(i18n.KeyDoctorShellDefault)}}
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
			detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorPortInUse), addr, err),
		}}
	}
	_ = listener.Close()
	return []checkResult{{
		name:   fmt.Sprintf("port %s", label),
		status: "OK",
		detail: fmt.Sprintf(i18n.TL(i18n.KeyDoctorPortAvail), addr),
	}}
}
