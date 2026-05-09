package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
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
	LogLevel  string          `mapstructure:"log_level"`
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
}

type TransportConfig struct {
	Stdio StdioConfig `mapstructure:"stdio"`
	SSH   SSHConfig   `mapstructure:"ssh"`
	MQTT  MQTTConfig  `mapstructure:"mqtt"`
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

type MCPConfig struct {
	Shell   ShellConfig                `mapstructure:"shell"`
	CDP     CDPConfig                  `mapstructure:"cdp"`
	Servers map[string]MCPServerConfig `mapstructure:"servers"`
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
}

type CDPConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Headless bool   `mapstructure:"headless"`
	WsURL    string `mapstructure:"ws_url"`
}

type PoolConfig struct {
	MaxConcurrency    int    `mapstructure:"max_concurrency"`
	DefaultTimeout    int    `mapstructure:"default_timeout"`
	WorkspaceDir      string `mapstructure:"workspace_dir"`
	IdleTimeout       int    `mapstructure:"idle_timeout"`
	MaxPendingResults int    `mapstructure:"max_pending_results"`
}

type SkillsConfig struct {
	Dir    string `mapstructure:"dir"`     // skills directory (default: .dolphinzZ/skills)
	MaxTop int    `mapstructure:"max_top"` // number of top skills to show in prompt (default: 10)
}

func Load(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigName(ConfigFileName)
	v.SetConfigType("yaml")

	// Load order: cfgFile > system < user < project (each overrides previous)
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		userConfigDir, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(userConfigDir, UserConfigDir))
		}
		v.AddConfigPath(SystemConfigDir)
		v.AddConfigPath(ProjectConfigDir)
	}

	// Env vars: DZ_LLM_MODEL -> llm.model
	v.SetEnvPrefix("DZ")
	v.AutomaticEnv()

	// Defaults
	setDefaults(v)

	// Read config (allow not found)
	if err := v.ReadInConfig(); err != nil {
		var nfe viper.ConfigFileNotFoundError
		if !errors.As(err, &nfe) {
			return nil, err
		}
		slog.Warn("no config file found, using defaults and env vars")
	}

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
	if v := os.Getenv("DZ_MQTT_BROKER"); v != "" {
		cfg.Transport.MQTT.Broker = v
	}
	if v := os.Getenv("DZ_MQTT_TOPIC"); v != "" {
		cfg.Transport.MQTT.Topic = v
	}
	if v := os.Getenv("DZ_MQTT_RESPONSE_TOPIC"); v != "" {
		cfg.Transport.MQTT.ResponseTopic = v
	}
	if v := os.Getenv("DZ_SESSION_MAX_AGE"); v != "" {
		cfg.Session.MaxAge = v
	}

	// Auto-generate SSH password if empty
	if cfg.Transport.SSH.Enabled && cfg.Transport.SSH.Password == "" {
		hd, _ := os.UserHomeDir()
		pwFile := filepath.Join(hd, UserConfigDir, "ssh_password")
		if data, err := os.ReadFile(pwFile); err == nil && len(data) > 0 {
			cfg.Transport.SSH.Password = string(data)
		} else {
			buf := make([]byte, 6)
			rand.Read(buf)
			cfg.Transport.SSH.Password = hex.EncodeToString(buf)
			os.MkdirAll(filepath.Dir(pwFile), 0700)
			os.WriteFile(pwFile, []byte(cfg.Transport.SSH.Password), 0600)
			fmt.Fprintf(os.Stderr, "\n=== SSH auto-generated password: %s ===\n", cfg.Transport.SSH.Password)
			fmt.Fprintf(os.Stderr, "Username: %s\n", cfg.Transport.SSH.Username)
			fmt.Fprintf(os.Stderr, "WARNING: Password stored in plaintext at: %s\n", pwFile)
			fmt.Fprintf(os.Stderr, "For better security, configure SSH key authentication.\n")
		}
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

	v.SetDefault("session.max_age", "24h")

	v.SetDefault("mcp.shell.enabled", true)
	v.SetDefault("mcp.shell.timeout_seconds", 30)
	v.SetDefault("mcp.cdp.enabled", true)
	v.SetDefault("mcp.cdp.headless", true)

	v.SetDefault("agent_pool.max_concurrency", 5)
	v.SetDefault("agent_pool.default_timeout", 300)
	v.SetDefault("agent_pool.workspace_dir", ".dolphinzZ/workspaces")
	v.SetDefault("agent_pool.idle_timeout", 600)
	v.SetDefault("agent_pool.max_pending_results", 10)

	v.SetDefault("skills.dir", ".dolphinzZ/skills")
	v.SetDefault("skills.max_top", 10)

	v.SetDefault("log_level", "info")
}
