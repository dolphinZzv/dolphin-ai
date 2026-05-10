package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var (
	SystemConfigDir  = "/etc/dolphinzZ"
	UserConfigDir    = ".dolphinzZ"
	ProjectConfigDir = ".dolphinzZ"
	ConfigFileName   = "config"
)

type Config struct {
	LLM       LLMConfig       `mapstructure:"llm"`
	Session   SessionConfig   `mapstructure:"session"`
	Transport TransportConfig `mapstructure:"transport"`
	MCP       MCPConfig       `mapstructure:"mcp"`
	Pool      PoolConfig      `mapstructure:"agent_pool"`
	Skills    SkillsConfig    `mapstructure:"skills"`
	Crontab   CrontabConfig   `mapstructure:"crontab"`
	Pprof     PprofConfig     `mapstructure:"pprof"`
	Metrics   MetricsConfig   `mapstructure:"metrics"`
	LogLevel  string          `mapstructure:"log_level"`
	LogFile   string          `mapstructure:"log_file"`
}

type LLMConfig struct {
	Type             string  `mapstructure:"type"` // "openai" or "anthropic"
	BaseURL          string  `mapstructure:"base_url"`
	APIKey           string  `mapstructure:"api_key"`
	Model            string  `mapstructure:"model"`
	MaxTokens        int     `mapstructure:"max_tokens"`
	MaxContextTokens int     `mapstructure:"max_context_tokens"` // context window limit before compression
	Temperature      float64 `mapstructure:"temperature"`
	MaxSubTurns      int     `mapstructure:"max_sub_turns"`
}

type SessionConfig struct {
	Dir     string `mapstructure:"dir"`
	MaxLoop int    `mapstructure:"max_loop"`
	Summary bool   `mapstructure:"summary"`
	MaxAge  string `mapstructure:"max_age"` // session file max age (e.g. "24h") for reaper
	Resume  bool   `mapstructure:"resume"`  // resume latest session on startup
}

type TransportConfig struct {
	Stdio StdioConfig `mapstructure:"stdio"`
	SSH   SSHConfig   `mapstructure:"ssh"`
	MQTT  MQTTConfig  `mapstructure:"mqtt"`
	Email EmailConfig `mapstructure:"email"`
}

type StdioConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type SSHConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	HostKey  string `mapstructure:"host_key"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type MQTTConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Broker        string `mapstructure:"broker"`
	Topic         string `mapstructure:"topic"`
	ResponseTopic string `mapstructure:"response_topic"`
	ClientID      string `mapstructure:"client_id"`
}

type EmailConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	IMAPHost     string `mapstructure:"imap_host"`
	IMAPPort     int    `mapstructure:"imap_port"`
	Username     string `mapstructure:"username"`
	Password     string `mapstructure:"password"`
	From         string `mapstructure:"from"`
	UseTLS       bool   `mapstructure:"use_tls"`
	PollInterval string `mapstructure:"poll_interval"` // IMAP poll interval, e.g. "10s"
}

type MCPConfig struct {
	Shell   ShellConfig                `mapstructure:"shell"`
	CDP     CDPConfig                  `mapstructure:"cdp"`
	Servers map[string]MCPServerConfig `mapstructure:"servers"`
	Repos   []string                   `mapstructure:"repos"` // manifest repos, e.g. ["dolphinzZv/mcp"]
}

type MCPServerConfig struct {
	Type    string   `mapstructure:"type"`    // "stdio" or "http"
	Command string   `mapstructure:"command"` // for stdio type
	Args    []string `mapstructure:"args"`    // for stdio type
	URL     string   `mapstructure:"url"`     // for http type
}

type ShellConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	AllowedCommands []string `mapstructure:"allowed_commands"` // empty = allow all
	TimeoutSeconds  int      `mapstructure:"timeout_seconds"`
	Priority        int      `mapstructure:"priority"` // tool listing priority (lower = preferred)
}

type CDPConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	Headless    bool   `mapstructure:"headless"`
	WsURL       string `mapstructure:"ws_url"`
	Priority    int    `mapstructure:"priority"`     // tool listing priority (lower = preferred)
	IdleTimeout int    `mapstructure:"idle_timeout"` // seconds, 0 = disabled
}

type PoolConfig struct {
	MaxConcurrency    int    `mapstructure:"max_concurrency"`
	DefaultTimeout    int    `mapstructure:"default_timeout"`
	WorkspaceDir      string `mapstructure:"workspace_dir"`
	IdleTimeout       int    `mapstructure:"idle_timeout"`
	MaxPendingResults int    `mapstructure:"max_pending_results"`
}

type SkillsConfig struct {
	Dir    string   `mapstructure:"dir"`     // skills directory (default: .dolphinzZ/skills)
	MaxTop int      `mapstructure:"max_top"` // number of top skills to show in prompt (default: 10)
	Repos  []string `mapstructure:"repos"`   // manifest repos, e.g. ["dolphinzZv/skills"]
}

type PprofConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"` // listen address, e.g. ":6060"
}

type CrontabConfig struct {
	File          string `mapstructure:"file"`
	CheckInterval string `mapstructure:"check_interval"` // e.g. "30s"
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"` // listen address, e.g. ":9090"
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	// Defaults (lowest priority)
	setDefaults(v)

	// Collect config files in priority order (each overrides the previous)
	var configFiles []string

	// 1. System config: /etc/dolphinzZ/config.yaml
	configFiles = append(configFiles, filepath.Join(SystemConfigDir, ConfigFileName+".yaml"))

	// 2. User config: ~/.dolphinzZ/config.yaml
	if homeDir, err := os.UserHomeDir(); err == nil {
		configFiles = append(configFiles, filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml"))
	}

	// 3. Project config: .dolphinzZ/config.yaml
	configFiles = append(configFiles, filepath.Join(ProjectConfigDir, ConfigFileName+".yaml"))

	// 4. -c flag (highest priority, overrides all)
	if cfgFile != "" {
		configFiles = append(configFiles, cfgFile)
	}

	// Read and merge each config file (skip missing)
	for _, f := range configFiles {
		data, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read config %s: %w", f, err)
		}
		v.SetConfigType(configType(f))
		if err := v.MergeConfig(bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("merge config %s: %w", f, err)
		}
		zap.S().Debugw("config merged", "file", f)
	}

	// Env vars: DZ_LLM_MODEL -> llm.model
	v.SetEnvPrefix("DZ")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Resolve session dir
	if cfg.Session.Dir == "" {
		cfg.Session.Dir = "/tmp/dolphinzZ"
	}

	// Manual env var overrides (Viper v1.18.2 env binding has issues)
	if v := os.Getenv("DZ_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("DZ_LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("DZ_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("DZ_LLM_TYPE"); v != "" {
		cfg.LLM.Type = v
	}
	if v := os.Getenv("DZ_LLM_MAX_TOKENS"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.LLM.MaxTokens)
	}
	if v := os.Getenv("DZ_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("DZ_LOG_FILE"); v != "" {
		cfg.LogFile = v
	}
	if v := os.Getenv("DZ_MQTT_BROKER"); v != "" {
		cfg.Transport.MQTT.Broker = v
	}
	if v := os.Getenv("DZ_MQTT_TOPIC"); v != "" {
		cfg.Transport.MQTT.Topic = v
	}
	if v := os.Getenv("DZ_MQTT_RESPONSE_TOPIC"); v != "" {
		cfg.Transport.MQTT.ResponseTopic = v
	}
	if v := os.Getenv("DZ_EMAIL_USERNAME"); v != "" {
		cfg.Transport.Email.Username = v
	}
	if v := os.Getenv("DZ_EMAIL_PASSWORD"); v != "" {
		cfg.Transport.Email.Password = v
	}
	if v := os.Getenv("DZ_SESSION_MAX_AGE"); v != "" {
		cfg.Session.MaxAge = v
	}
	if v := os.Getenv("DZ_TRANSPORT_STDIO_ENABLED"); v != "" {
		cfg.Transport.Stdio.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("DZ_TRANSPORT_MQTT_ENABLED"); v != "" {
		cfg.Transport.MQTT.Enabled = v == "true" || v == "1"
	}

	// Auto-generate SSH password if empty
	if cfg.Transport.SSH.Enabled && cfg.Transport.SSH.Password == "" {
		hd, _ := os.UserHomeDir()
		pwFile := filepath.Join(hd, UserConfigDir, "ssh_password")
		if data, err := os.ReadFile(pwFile); err == nil && len(data) > 0 {
			cfg.Transport.SSH.Password = string(data)
		} else {
			buf := make([]byte, 16)
			rand.Read(buf)
			cfg.Transport.SSH.Password = hex.EncodeToString(buf)
			os.MkdirAll(filepath.Dir(pwFile), 0700)
			os.WriteFile(pwFile, []byte(cfg.Transport.SSH.Password), 0600)
			fmt.Fprintf(os.Stderr, "\n=== SSH auto-generated password saved to: %s ===\n", pwFile)
			fmt.Fprintf(os.Stderr, "Username: %s\n", cfg.Transport.SSH.Username)
			fmt.Fprintf(os.Stderr, "WARNING: Password stored in plaintext. For better security, configure SSH key authentication.\n")
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

// DefaultConfig returns a Config with default values (useful for tests).
func DefaultConfig() *Config {
	v := viper.New()
	setDefaults(v)
	var cfg Config
	v.Unmarshal(&cfg)
	cfg.Session.Dir = "/tmp/dolphinzZ"
	return &cfg
}

// ToolSelection is a lightweight config fragment for saving skill/MCP tool choices.
type ToolSelection struct {
	Skills []string `yaml:"skills"`
	MCP    []string `yaml:"mcp"`
}

// LoadedConfig is the structure written to config files for tool loading.
type LoadedConfig struct {
	Skills LoadedTools `yaml:"skills"`
	MCP    LoadedTools `yaml:"mcp"`
}

// LoadedTools holds the list of loaded tool names.
type LoadedTools struct {
	Loaded []string `yaml:"loaded"`
}

// SaveToolSelection persists the user's tool choices to the config file at the given
// scope ("user" or "project"). It merges with existing loaded tools if any.
func SaveToolSelection(selection *ToolSelection, scope string) error {
	var configPath string
	if scope == "user" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		configPath = filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml")
	} else {
		configPath = filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")
	}

	// Read existing loaded config
	existing := LoadedConfig{}
	if data, err := os.ReadFile(configPath); err == nil {
		yaml.Unmarshal(data, &existing)
	}

	// Merge selections, deduplicate
	skillSet := make(map[string]bool)
	for _, s := range existing.Skills.Loaded {
		skillSet[s] = true
	}
	for _, s := range selection.Skills {
		skillSet[s] = true
	}
	existing.Skills.Loaded = make([]string, 0, len(skillSet))
	for s := range skillSet {
		existing.Skills.Loaded = append(existing.Skills.Loaded, s)
	}

	mcpSet := make(map[string]bool)
	for _, m := range existing.MCP.Loaded {
		mcpSet[m] = true
	}
	for _, m := range selection.MCP {
		mcpSet[m] = true
	}
	existing.MCP.Loaded = make([]string, 0, len(mcpSet))
	for m := range mcpSet {
		existing.MCP.Loaded = append(existing.MCP.Loaded, m)
	}

	data, err := yaml.Marshal(&existing)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Append to existing file (preserve other settings)
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	// Write a separator and the new section
	f.WriteString("\n# Tool selections (auto-generated)\n")
	f.Write(data)
	return nil
}

func configType(path string) string {
	switch filepath.Ext(path) {
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	default:
		return "yaml"
	}
}

// Validate checks the configuration and returns an error for invalid settings.
func (c *Config) Validate() error {
	if c.LLM.Type != "" && c.LLM.Type != "openai" && c.LLM.Type != "anthropic" {
		return fmt.Errorf(`llm.type must be "openai" or "anthropic", got %q`, c.LLM.Type)
	}
	if c.LLM.MaxTokens <= 0 {
		return fmt.Errorf("llm.max_tokens must be > 0")
	}
	if c.LLM.MaxContextTokens <= 0 {
		return fmt.Errorf("llm.max_context_tokens must be > 0")
	}
	if c.LLM.MaxSubTurns <= 0 {
		return fmt.Errorf("llm.max_sub_turns must be > 0")
	}
	if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
		return fmt.Errorf("llm.temperature must be between 0 and 2")
	}
	if c.Session.MaxLoop <= 0 {
		return fmt.Errorf("session.max_loop must be > 0")
	}
	if c.Pool.MaxConcurrency <= 0 {
		return fmt.Errorf("agent_pool.max_concurrency must be > 0")
	}
	if c.Pool.DefaultTimeout <= 0 {
		return fmt.Errorf("agent_pool.default_timeout must be > 0")
	}
	if c.Pool.MaxPendingResults <= 0 {
		return fmt.Errorf("agent_pool.max_pending_results must be > 0")
	}
	if c.MCP.Shell.TimeoutSeconds <= 0 {
		return fmt.Errorf("mcp.shell.timeout_seconds must be > 0")
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("llm.type", "openai")
	v.SetDefault("llm.base_url", "https://api.openai.com/v1")
	v.SetDefault("llm.model", "gpt-4o")
	v.SetDefault("llm.max_tokens", 4096)
	v.SetDefault("llm.temperature", 0.7)
	v.SetDefault("llm.max_sub_turns", 10)
	v.SetDefault("llm.max_context_tokens", 1048576)

	v.SetDefault("session.dir", "/tmp/dolphinzZ")
	v.SetDefault("session.max_loop", 50)
	v.SetDefault("session.summary", true)

	v.SetDefault("transport.stdio.enabled", true)
	v.SetDefault("transport.ssh.enabled", false)
	v.SetDefault("transport.ssh.addr", ":2222")
	v.SetDefault("transport.ssh.host_key", "~/.ssh/id_ed25519")
	v.SetDefault("transport.ssh.username", "dolphinzZ")
	v.SetDefault("transport.ssh.password", "")
	v.SetDefault("transport.mqtt.enabled", false)
	v.SetDefault("transport.mqtt.broker", "tcp://localhost:1883")
	v.SetDefault("transport.mqtt.topic", "dolphinzZ/agent/command")
	v.SetDefault("transport.mqtt.response_topic", "dolphinzZ/agent/response")
	v.SetDefault("transport.mqtt.client_id", "dolphinzZ-agent")

	v.SetDefault("transport.email.enabled", false)
	v.SetDefault("transport.email.smtp_port", 587)
	v.SetDefault("transport.email.imap_port", 993)
	v.SetDefault("transport.email.use_tls", true)
	v.SetDefault("transport.email.poll_interval", "10s")

	v.SetDefault("session.max_age", "24h")
	v.SetDefault("session.resume", false)

	v.SetDefault("mcp.shell.enabled", true)
	v.SetDefault("mcp.shell.timeout_seconds", 30)
	v.SetDefault("mcp.shell.priority", 10)
	v.SetDefault("mcp.cdp.enabled", true)
	v.SetDefault("mcp.cdp.headless", true)
	v.SetDefault("mcp.cdp.priority", 1000)
	v.SetDefault("mcp.cdp.idle_timeout", 300)

	v.SetDefault("agent_pool.max_concurrency", 5)
	v.SetDefault("agent_pool.default_timeout", 300)
	v.SetDefault("agent_pool.workspace_dir", ".dolphinzZ/workspaces")
	v.SetDefault("agent_pool.idle_timeout", 600)
	v.SetDefault("agent_pool.max_pending_results", 10)

	v.SetDefault("skills.dir", ".dolphinzZ/skills")
	v.SetDefault("skills.max_top", 10)
	v.SetDefault("skills.repos", []string{})

	v.SetDefault("crontab.file", ".dolphinzZ/CRONTAB.md")
	v.SetDefault("crontab.check_interval", "30s")

	v.SetDefault("pprof.enabled", false)
	v.SetDefault("pprof.addr", ":6060")

	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.addr", ":9090")

	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", ".dolphinzZ/logs/agent.log")
}
