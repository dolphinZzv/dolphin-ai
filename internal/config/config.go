// Package config manages dolphin configuration loading, validation, and persistence.
package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var (
	SystemConfigDir  = defaultSystemConfigDir()
	UserConfigDir    = ".dolphin"
	ProjectConfigDir = ".dolphin"
	ConfigFileName   = "config"
)

type Config struct {
	Name        string            `mapstructure:"name"`
	ID          string            `mapstructure:"id"`
	LLM         LLMConfig         `mapstructure:"llm"`
	Session     SessionConfig     `mapstructure:"session"`
	Transport   TransportConfig   `mapstructure:"transport"`
	Servers     ServersConfig     `mapstructure:"servers"`
	MCP         MCPConfig         `mapstructure:"mcp"`
	Pool        PoolConfig        `mapstructure:"agent_pool"`
	Skills      SkillsConfig      `mapstructure:"skills"`
	Workflows   WorkflowsConfig   `mapstructure:"workflows"`
	Agents      AgentsConfig      `mapstructure:"agents"`
	Crontab     CrontabConfig     `mapstructure:"crontab"`
	Pprof       PprofConfig       `mapstructure:"pprof"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
	Health      HealthConfig      `mapstructure:"health"`
	Telemetry   TelemetryConfig   `mapstructure:"telemetry"`
	Diary       DiaryConfig       `mapstructure:"diary"`
	Update      UpdateConfig      `mapstructure:"update"`
	LogLevel    string            `mapstructure:"log_level"`
	LogFile     string            `mapstructure:"log_file"`
	LogMaxSize  int               `mapstructure:"log_max_size"`
	LogMaxAge   int               `mapstructure:"log_max_age"`
	LogMaxBack  int               `mapstructure:"log_max_backup"`
	Plugins     PluginsConfig     `mapstructure:"plugins"`
	Flags       FlagsConfig       `mapstructure:"flags"`
	Resource    ResourceConfig    `mapstructure:"resource"`
	Credentials CredentialsConfig `mapstructure:"credentials"`
	SyncConfig  bool              `mapstructure:"sync_config"`
}

// Clone deep-copies the Config using JSON round-trip.
// The returned Config is safe to mutate independently.
// Falls back to DefaultConfig on marshal/unmarshal errors to avoid nil panics.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		zap.S().Errorw("config clone marshal failed, falling back to default", "error", err)
		return DefaultConfig()
	}
	var cloned Config
	if err := json.Unmarshal(data, &cloned); err != nil {
		zap.S().Errorw("config clone unmarshal failed, falling back to default", "error", err)
		return DefaultConfig()
	}
	return &cloned
}

var sessionsDirOverride string

// SetSessionsDir overrides the sessions directory (for testing).
func SetSessionsDir(dir string) { sessionsDirOverride = dir }

// SessionsDir returns the sessions directory, preferring project-level (.dolphin/sessions/)
// and falling back to user-level (~/.dolphin/sessions/).
func SessionsDir() string {
	if sessionsDirOverride != "" {
		return sessionsDirOverride
	}
	if _, err := os.Stat(ProjectConfigDir); err == nil {
		return filepath.Join(ProjectConfigDir, "sessions")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(UserConfigDir, "sessions")
	}
	return filepath.Join(homeDir, UserConfigDir, "sessions")
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	// Defaults (lowest priority)
	setDefaults(v)

	// Collect config files in priority order (each overrides the previous)
	var configFiles []string

	// 1. System config: /etc/dolphin/config.yaml
	configFiles = append(configFiles, filepath.Join(SystemConfigDir, ConfigFileName+".yaml"))

	// 2. User config: ~/.dolphin/config.yaml
	if homeDir, err := os.UserHomeDir(); err == nil {
		configFiles = append(configFiles, filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml"))
	}

	// 3. Project config: .dolphin/config.yaml
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

	// Manual env var overrides (Viper v1.18.2 env binding has issues)
	if v := os.Getenv("DZ_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if cfg.LLM.APIKey != "" {
		for i := range cfg.LLM.Providers {
			if cfg.LLM.Providers[i].APIKey == "" {
				cfg.LLM.Providers[i].APIKey = cfg.LLM.APIKey
			}
		}
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
		_, _ = fmt.Sscanf(v, "%d", &cfg.LLM.MaxTokens)
	}
	if cfg.LLM.MaxTokens > 0 {
		for i := range cfg.LLM.Providers {
			if cfg.LLM.Providers[i].MaxTokens <= 0 {
				cfg.LLM.Providers[i].MaxTokens = cfg.LLM.MaxTokens
			}
		}
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
		cfg.Transport.MQTT.SubscribeTopic = v
	}
	if v := os.Getenv("DZ_MQTT_PUBLISH_TOPIC"); v != "" {
		cfg.Transport.MQTT.PublishTopic = v
	}
	if v := os.Getenv("DZ_EMAIL_USERNAME"); v != "" {
		cfg.Transport.Email.Username = v
	}
	if v := os.Getenv("DZ_EMAIL_PASSWORD"); v != "" {
		cfg.Transport.Email.Password = v
	}
	if v := os.Getenv("DZ_DINGTALK_CLIENT_ID"); v != "" {
		cfg.Transport.DingTalk.ClientID = v
	}
	if v := os.Getenv("DZ_DINGTALK_CLIENT_SECRET"); v != "" {
		cfg.Transport.DingTalk.ClientSecret = v
	}
	if v := os.Getenv("DZ_DINGTALK_ENABLED"); v != "" {
		cfg.Transport.DingTalk.Enabled = v == "true" || v == "1"
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
	if v := os.Getenv("DZ_SERVERS_MQTT_BROKER_ENABLED"); v != "" {
		cfg.Servers.MQTTBroker.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("DZ_SERVERS_MQTT_BROKER_ADDR"); v != "" {
		cfg.Servers.MQTTBroker.Addr = v
	}
	if v := os.Getenv("DZ_SERVERS_MQTT_BROKER_USER"); v != "" {
		if len(cfg.Servers.MQTTBroker.Accounts) == 0 {
			cfg.Servers.MQTTBroker.Accounts = append(cfg.Servers.MQTTBroker.Accounts, MQTTAccount{Username: v})
		} else {
			cfg.Servers.MQTTBroker.Accounts[0].Username = v
		}
	}
	if v := os.Getenv("DZ_SERVERS_MQTT_BROKER_PASSWORD"); v != "" {
		if len(cfg.Servers.MQTTBroker.Accounts) == 0 {
			cfg.Servers.MQTTBroker.Accounts = append(cfg.Servers.MQTTBroker.Accounts, MQTTAccount{Password: v})
		} else {
			cfg.Servers.MQTTBroker.Accounts[0].Password = v
		}
	}

	if v := os.Getenv("DZ_UPDATE_ENABLED"); v != "" {
		cfg.Update.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("DZ_UPDATE_CHECK_INTERVAL"); v != "" {
		cfg.Update.CheckInterval = v
	}
	if v := os.Getenv("DZ_UPDATE_CHANNEL"); v != "" {
		cfg.Update.Channel = v
	}
	if v := os.Getenv("DZ_UPDATE_AUTO_INSTALL"); v != "" {
		cfg.Update.AutoInstall = v == "true" || v == "1"
	}

	// Auto-generate MQTT broker account if broker is enabled and no accounts configured.
	if cfg.Servers.MQTTBroker.Enabled && len(cfg.Servers.MQTTBroker.Accounts) == 0 {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err == nil {
			cfg.Servers.MQTTBroker.Accounts = []MQTTAccount{{
				Username: "dolphin",
				Password: hex.EncodeToString(buf),
			}}
		}
	}

	// When MQTT broker is enabled and transport MQTT client has no broker set,
	// point the client at the embedded broker and auto-populate credentials.
	if cfg.Servers.MQTTBroker.Enabled && cfg.Transport.MQTT.Enabled {
		if cfg.Transport.MQTT.Broker == "" || cfg.Transport.MQTT.Broker == "tcp://localhost:1883" {
			addr := cfg.Servers.MQTTBroker.Addr
			if addr == "" {
				addr = ":1883"
			}
			cfg.Transport.MQTT.Broker = fmt.Sprintf("tcp://%s", clientAddrFromAddr(addr))
		}
		if cfg.Transport.MQTT.Username == "" && len(cfg.Servers.MQTTBroker.Accounts) > 0 {
			cfg.Transport.MQTT.Username = cfg.Servers.MQTTBroker.Accounts[0].Username
			cfg.Transport.MQTT.Password = cfg.Servers.MQTTBroker.Accounts[0].Password
		}
	}

	// Auto-generate SSH password if empty. Fails closed — if generation fails,
	// the SSH transport will refuse to start (checked in NewSSHTransport).
	if cfg.Transport.SSH.Enabled && cfg.Transport.SSH.Password == "" {
		hd, err := os.UserHomeDir()
		if err != nil {
			hd = os.TempDir()
		}
		pwFile := filepath.Join(hd, UserConfigDir, "ssh_password")
		if data, err := os.ReadFile(pwFile); err == nil && len(data) > 0 {
			cfg.Transport.SSH.Password = string(data)
		} else {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: failed to generate SSH password: %v\n", err)
				cfg.Transport.SSH.Password = ""
			} else {
				cfg.Transport.SSH.Password = hex.EncodeToString(buf)
				if err := os.MkdirAll(filepath.Dir(pwFile), 0700); err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: failed to create SSH password directory: %v\n", err)
					cfg.Transport.SSH.Password = ""
				} else if err := os.WriteFile(pwFile, []byte(cfg.Transport.SSH.Password), 0600); err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: failed to write SSH password: %v\n", err)
					cfg.Transport.SSH.Password = ""
				} else {
					fmt.Fprintf(os.Stderr, "\n=== SSH auto-generated password saved to: %s ===\n", pwFile)
					fmt.Fprintf(os.Stderr, "Username: %s\n", cfg.Transport.SSH.Username)
					fmt.Fprintf(os.Stderr, "WARNING: Password stored in plaintext. For better security, configure SSH key authentication.\n")
				}
			}
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	if cfg.SyncConfig {
		if err := fillConfigDefaults(); err != nil {
			zap.S().Warnw("failed to sync config defaults", "error", err)
		}
	}

	return &cfg, nil
}

// fillConfigDefaults reads the project config file and fills in missing
// fields with their default values. Existing values are preserved.
func fillConfigDefaults() error {
	path := filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config for sync: %w", err)
	}

	var current map[string]any
	if err := yaml.Unmarshal(data, &current); err != nil {
		return fmt.Errorf("unmarshal config for sync: %w", err)
	}

	v := viper.New()
	setDefaults(v)
	defaults := v.AllSettings()

	merged := deepMergeDefaults(current, defaults)

	out, err := yaml.Marshal(merged)
	if err != nil {
		return fmt.Errorf("marshal config with defaults: %w", err)
	}

	if string(out) != string(data) {
		if err := os.WriteFile(path, out, 0600); err != nil {
			return fmt.Errorf("write synced config: %w", err)
		}
		zap.S().Infow("config defaults synced", "path", path)
	}
	return nil
}

// deepMergeDefaults recursively merges default values into the current config map.
// Current values are preserved; only missing keys are filled from defaults.
func deepMergeDefaults(current, defaults map[string]any) map[string]any {
	if current == nil {
		return defaults
	}
	for k, dv := range defaults {
		if _, exists := current[k]; !exists {
			current[k] = dv
		} else {
			cm, cOk := current[k].(map[string]any)
			dm, dOk := dv.(map[string]any)
			if cOk && dOk {
				current[k] = deepMergeDefaults(cm, dm)
			}
		}
	}
	return current
}

// DefaultConfig returns a Config with default values (useful for tests).
func DefaultConfig() *Config {
	v := viper.New()
	setDefaults(v)
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		zap.S().Errorw("unmarshal default config", "error", err)
	}
	if cfg.ID == "" {
		cfg.ID = LoadOrCreateDolphinID()
	}
	return &cfg
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

	existing := LoadedConfig{}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &existing)
	}

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

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	full := make(map[string]any)
	if existingData, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(existingData, &full)
	}
	fullSkills, ok := full["skills"].(map[string]any)
	if !ok {
		fullSkills = make(map[string]any)
		full["skills"] = fullSkills
	}
	fullSkills["loaded"] = existing.Skills.Loaded

	fullMCP, ok := full["mcp"].(map[string]any)
	if !ok {
		fullMCP = make(map[string]any)
		full["mcp"] = fullMCP
	}
	fullMCP["loaded"] = existing.MCP.Loaded
	out, err := yaml.Marshal(full)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configPath, out, 0600)
}

// clientAddrFromAddr resolves a listen address to a client-connectable address.
// When the address listens on all interfaces (":port" or "0.0.0.0:port"), returns "localhost:port".
func clientAddrFromAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "localhost:1883"
	}
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
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

// LLMConfigured returns true if the config has at least one LLM provider with an API key.
func (c *Config) LLMConfigured() bool {
	if len(c.LLM.Providers) > 0 {
		for _, p := range c.LLM.Providers {
			if p.APIKey != "" {
				return true
			}
		}
		return false
	}
	return c.LLM.APIKey != ""
}

// Validate checks the configuration and returns an error for invalid settings.
func (c *Config) Validate() error {
	if len(c.LLM.Providers) > 0 {
		for i, p := range c.LLM.Providers {
			if p.Type != "" && p.Type != "openai" && p.Type != "anthropic" {
				return fmt.Errorf(`llm.providers[%d].type must be "openai" or "anthropic", got %q (check your config file)`, i, p.Type)
			}
			if p.APIKey == "" {
				return fmt.Errorf("llm.providers[%d].api_key is required — set via config file or DZ_LLM_API_KEY env var", i)
			}
			if p.Model == "" {
				return fmt.Errorf("llm.providers[%d].model is required — set via config file or DZ_LLM_MODEL env var", i)
			}
		}
	} else {
		if c.LLM.Type != "" && c.LLM.Type != "openai" && c.LLM.Type != "anthropic" {
			return fmt.Errorf(`llm.type must be "openai" or "anthropic", got %q (check your config file)`, c.LLM.Type)
		}
	}
	if c.LLM.MaxTokens <= 0 {
		return fmt.Errorf("llm.max_tokens must be > 0, got %d (set in config or DZ_LLM_MAX_TOKENS env var)", c.LLM.MaxTokens)
	}
	if c.LLM.MaxContextTokens <= 0 {
		return fmt.Errorf("llm.max_context_tokens must be > 0, got %d", c.LLM.MaxContextTokens)
	}
	if c.LLM.MaxSubTurns <= 0 {
		return fmt.Errorf("llm.max_sub_turns must be > 0, got %d", c.LLM.MaxSubTurns)
	}
	if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
		return fmt.Errorf("llm.temperature must be between 0 and 2, got %.1f", c.LLM.Temperature)
	}
	if c.Session.MaxLoop <= 0 {
		return fmt.Errorf("session.max_loop must be > 0, got %d", c.Session.MaxLoop)
	}
	if c.Pool.MaxConcurrency <= 0 {
		return fmt.Errorf("agent_pool.max_concurrency must be > 0, got %d", c.Pool.MaxConcurrency)
	}
	if c.Pool.DefaultTimeout <= 0 {
		return fmt.Errorf("agent_pool.default_timeout must be > 0, got %d", c.Pool.DefaultTimeout)
	}
	if c.Pool.MaxPendingResults <= 0 {
		return fmt.Errorf("agent_pool.max_pending_results must be > 0, got %d", c.Pool.MaxPendingResults)
	}
	if c.Pool.MaxPendingResultLen <= 0 {
		c.Pool.MaxPendingResultLen = 500
	}
	if c.MCP.Shell.TimeoutSeconds <= 0 {
		return fmt.Errorf("mcp.shell.timeout_seconds must be > 0, got %d", c.MCP.Shell.TimeoutSeconds)
	}
	return nil
}
