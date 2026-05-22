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
	"time"

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
	Name       string          `mapstructure:"name"`
	ID         string          `mapstructure:"id"`
	LLM        LLMConfig       `mapstructure:"llm"`
	Session    SessionConfig   `mapstructure:"session"`
	Transport  TransportConfig `mapstructure:"transport"`
	Servers    ServersConfig   `mapstructure:"servers"`
	MCP        MCPConfig       `mapstructure:"mcp"`
	Pool       PoolConfig      `mapstructure:"agent_pool"`
	Skills     SkillsConfig    `mapstructure:"skills"`
	Agents     AgentsConfig    `mapstructure:"agents"`
	Crontab    CrontabConfig   `mapstructure:"crontab"`
	Pprof      PprofConfig     `mapstructure:"pprof"`
	Metrics    MetricsConfig   `mapstructure:"metrics"`
	Health     HealthConfig    `mapstructure:"health"`
	Telemetry  TelemetryConfig `mapstructure:"telemetry"`
	Diary      DiaryConfig     `mapstructure:"diary"`
	Update     UpdateConfig    `mapstructure:"update"`
	LogLevel   string          `mapstructure:"log_level"`
	LogFile    string          `mapstructure:"log_file"`
	LogMaxSize int             `mapstructure:"log_max_size"`
	LogMaxAge  int             `mapstructure:"log_max_age"`
	LogMaxBack int             `mapstructure:"log_max_backup"`
	Plugins    PluginsConfig   `mapstructure:"plugins"`
	Flags      FlagsConfig     `mapstructure:"flags"`
	Resource   ResourceConfig  `mapstructure:"resource"`
	SyncConfig bool            `mapstructure:"sync_config"`
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

// ProviderConfig defines a single LLM provider endpoint.
type ProviderConfig struct {
	Name      string `mapstructure:"name"`
	Type      string `mapstructure:"type"` // "openai" or "anthropic"
	BaseURL   string `mapstructure:"base_url"`
	APIKey    string `mapstructure:"api_key"`
	Model     string `mapstructure:"model"`
	MaxTokens int    `mapstructure:"max_tokens"`
}

type LLMConfig struct {
	// Legacy single-provider fields (populated by env vars for backward compat).
	Type      string `mapstructure:"type"`
	BaseURL   string `mapstructure:"base_url"`
	APIKey    string `mapstructure:"api_key"`
	Model     string `mapstructure:"model"`
	MaxTokens int    `mapstructure:"max_tokens"`

	// Agent-level settings (shared regardless of which provider is active).
	MaxContextTokens  int     `mapstructure:"max_context_tokens"`
	Temperature       float64 `mapstructure:"temperature"`
	MaxSubTurns       int     `mapstructure:"max_sub_turns"`
	CompressMode      string  `mapstructure:"compress_mode"`
	SegmentMergeLimit int     `mapstructure:"segment_merge_limit"`

	// Multi-provider: if set, startup selects the first that passes health check.
	Providers []ProviderConfig `mapstructure:"providers"`

	Limits LimitsConfig `mapstructure:"limits"`
}

// EffectiveProviders returns the list of provider configs to try at startup.
// If multi-provider config is set, it returns those. Otherwise, it builds a
// single entry from the legacy fields (env var overrides).
func (l *LLMConfig) EffectiveProviders() []ProviderConfig {
	if len(l.Providers) > 0 {
		return l.Providers
	}
	return []ProviderConfig{{
		Name:      "default",
		Type:      l.Type,
		BaseURL:   l.BaseURL,
		APIKey:    l.APIKey,
		Model:     l.Model,
		MaxTokens: l.MaxTokens,
	}}
}

type SessionConfig struct {
	MaxLoop int    `mapstructure:"max_loop"`
	Summary bool   `mapstructure:"summary"`
	MaxAge  string `mapstructure:"max_age"`
	Resume  bool   `mapstructure:"resume"`
}

type LimitsConfig struct {
	Enabled          bool             `mapstructure:"enabled"`
	SchedulerEnabled bool             `mapstructure:"scheduler_enabled"`
	Requests         MultiLevelLimits `mapstructure:"requests"`
	Tokens           TokenMultiLimits `mapstructure:"tokens"`
	Concurrency      ConcurrencyLimit `mapstructure:"concurrency"`
	Enforcement      string           `mapstructure:"enforcement"`
	Retry            RetryConfig      `mapstructure:"retry"`
	Exempt           ExemptConfig     `mapstructure:"exempt"`
	ProviderMode     string           `mapstructure:"provider_mode"`
}

type MultiLevelLimits struct {
	Daily   LevelLimit `mapstructure:"daily"`
	Weekly  LevelLimit `mapstructure:"weekly"`
	Monthly LevelLimit `mapstructure:"monthly"`
}

type LevelLimit struct {
	Max       int    `mapstructure:"max"`
	ResetCron string `mapstructure:"reset_cron"`
}

type TokenMultiLimits struct {
	Daily   TokenLevelLimit `mapstructure:"daily"`
	Weekly  TokenLevelLimit `mapstructure:"weekly"`
	Monthly TokenLevelLimit `mapstructure:"monthly"`
}

type TokenLevelLimit struct {
	InputMax  int    `mapstructure:"input_max"`
	OutputMax int    `mapstructure:"output_max"`
	ResetCron string `mapstructure:"reset_cron"`
}

type ConcurrencyLimit struct {
	MaxRunning int `mapstructure:"max_running"`
}

type RetryConfig struct {
	MaxAttempts    int           `mapstructure:"max_attempts"`
	InitialBackoff time.Duration `mapstructure:"initial_backoff"`
	MaxBackoff     time.Duration `mapstructure:"max_backoff"`
}

type ExemptConfig struct {
	Enabled  bool     `mapstructure:"enabled"`
	Patterns []string `mapstructure:"patterns"`
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

type TransportConfig struct {
	Stdio    StdioConfig    `mapstructure:"stdio"`
	SSH      SSHConfig      `mapstructure:"ssh"`
	MQTT     MQTTConfig     `mapstructure:"mqtt"`
	Email    EmailConfig    `mapstructure:"email"`
	DingTalk DingTalkConfig `mapstructure:"dingtalk"`
	ACP      ACPConfig      `mapstructure:"acp"`
	A2A      A2AConfig      `mapstructure:"a2a"`
}

// ServersConfig holds standalone server configurations (MQTT broker, etc.).
// These are independent of transport clients and run as in-process services.
type ServersConfig struct {
	MQTTBroker MQTTBrokerConfig `mapstructure:"mqtt_broker"`
}

// MQTTBrokerConfig configures an embedded MQTT broker server.
// Separate from transport.mqtt which configures the MQTT client.
type MQTTBrokerConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Addr     string        `mapstructure:"addr"`
	Accounts []MQTTAccount `mapstructure:"accounts"`
}

// ACPConfig holds configuration for the ACP (Agent Communication Protocol) transport.
// Uses REST over HTTP, following IBM BeeAI ACP specification.
type ACPConfig struct {
	Enabled      bool            `mapstructure:"enabled"`
	ListenAddr   string          `mapstructure:"listen_addr"`
	AgentID      string          `mapstructure:"agent_id"`
	AgentName    string          `mapstructure:"agent_name"`
	AgentVersion string          `mapstructure:"agent_version"`
	AgentDesc    string          `mapstructure:"agent_description"`
	Capabilities []string        `mapstructure:"capabilities"`
	SyncTimeout  string          `mapstructure:"sync_timeout"`
	APIKey       string          `mapstructure:"api_key"`
	TLSEnabled   bool            `mapstructure:"tls_enabled"`
	TLSCertFile  string          `mapstructure:"tls_cert_file"`
	TLSKeyFile   string          `mapstructure:"tls_key_file"`
	Peers        []ACPPeerConfig `mapstructure:"peers"`
}

type ACPPeerConfig struct {
	ID     string `mapstructure:"id"`
	URL    string `mapstructure:"url"`
	APIKey string `mapstructure:"api_key"`
}

// A2AConfig holds configuration for the A2A (Agent-to-Agent) transport.
// Uses JSON-RPC 2.0 over HTTP, following Google A2A specification.
type A2AConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	ListenAddr   string   `mapstructure:"listen_addr"`
	AgentID      string   `mapstructure:"agent_id"`
	AgentName    string   `mapstructure:"agent_name"`
	AgentVersion string   `mapstructure:"agent_version"`
	AgentDesc    string   `mapstructure:"agent_description"`
	Capabilities []string `mapstructure:"capabilities"`
	SyncTimeout  string   `mapstructure:"sync_timeout"`
	APIKey       string   `mapstructure:"api_key"`
	TLSEnabled   bool     `mapstructure:"tls_enabled"`
	TLSCertFile  string   `mapstructure:"tls_cert_file"`
	TLSKeyFile   string   `mapstructure:"tls_key_file"`
}

type StdioConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	MarkdownRender bool   `mapstructure:"markdown_render"`
	MarkdownStyle  string `mapstructure:"markdown_style"`
}

type SSHConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Addr           string `mapstructure:"addr"`
	HostKey        string `mapstructure:"host_key"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
	MarkdownRender bool   `mapstructure:"markdown_render"`
	MarkdownStyle  string `mapstructure:"markdown_style"`
}

type MQTTAccount struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type MQTTConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Broker         string `mapstructure:"broker"`
	SubscribeTopic string `mapstructure:"subscribe_topic"`
	PublishTopic   string `mapstructure:"publish_topic"`
	ClientID       string `mapstructure:"client_id"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
}

type EmailConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	Protocol       string   `mapstructure:"protocol"` // "imap" (default) or "pop3"
	SMTPHost       string   `mapstructure:"smtp_host"`
	SMTPPort       int      `mapstructure:"smtp_port"`
	IMAPHost       string   `mapstructure:"imap_host"`
	IMAPPort       int      `mapstructure:"imap_port"`
	POP3Host       string   `mapstructure:"pop3_host"` // defaults to IMAPHost / SMTPHost
	POP3Port       int      `mapstructure:"pop3_port"` // default 995 (TLS)
	Username       string   `mapstructure:"username"`
	Password       string   `mapstructure:"password"`
	From           string   `mapstructure:"from"`
	UseTLS         bool     `mapstructure:"use_tls"`
	SkipTLSVerify  bool     `mapstructure:"skip_tls_verify"` // skip TLS cert verification (e.g. self-signed certs)
	PollInterval   string   `mapstructure:"poll_interval"`   // IMAP poll interval, e.g. "10s"
	AllowedSenders []string `mapstructure:"allowed_senders"` // only process emails from these addresses
}

// DingTalkConfig holds configuration for the DingTalk bot transport.
// Uses Stream mode (WebSocket long connection) — no public IP or callback URL needed.
// The bot actively connects to DingTalk servers and receives messages via push.
type DingTalkConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	ClientID     string `mapstructure:"client_id"`     // AppKey from DingTalk Open Platform
	ClientSecret string `mapstructure:"client_secret"` // AppSecret from DingTalk Open Platform
}

type MCPConfig struct {
	Shell     ShellConfig                `mapstructure:"shell"`
	CDP       CDPConfig                  `mapstructure:"cdp"`
	Email     EmailMCPConfig             `mapstructure:"email"`
	Webhook   MCPWebhookConfig           `mapstructure:"webhook"`
	WebSearch MCPWebSearchConfig         `mapstructure:"web_search"`
	Servers   map[string]MCPServerConfig `mapstructure:"servers"`
	Repos     []string                   `mapstructure:"repos"` // manifest repos, e.g. ["dolphinv/mcp"]
}

type MCPWebhookConfig struct {
	Enabled  bool                     `mapstructure:"enabled"`
	Priority int                      `mapstructure:"priority"`
	Targets  map[string]WebhookTarget `mapstructure:"targets"` // named pre-configured webhook targets
}

type WebhookTarget struct {
	URL     string            `mapstructure:"url"`
	Method  string            `mapstructure:"method"`  // HTTP method, e.g. "POST" (default), "GET"
	Headers map[string]string `mapstructure:"headers"` // custom HTTP headers
}

type MCPWebSearchConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Priority  int      `mapstructure:"priority"`
	Provider  string   `mapstructure:"provider"`  // single default provider (backward compat)
	Providers []string `mapstructure:"providers"` // enabled provider list, intersected with registered providers for LLM enum
	APIKey    string   `mapstructure:"api_key"`   // for serper/iflow providers
}

type MCPServerConfig struct {
	Type    string            `mapstructure:"type"`    // "stdio", "sse", "http-stream"
	Command string            `mapstructure:"command"` // for stdio type
	Args    []string          `mapstructure:"args"`    // for stdio type
	URL     string            `mapstructure:"url"`     // for sse / http-stream type
	Headers map[string]string `mapstructure:"headers"` // custom HTTP headers (auth etc.)
	Timeout int               `mapstructure:"timeout"` // request timeout in seconds, 0 = default 30
	Enabled *bool             `mapstructure:"enabled"` // nil or true = enabled, false = skip
}

// TimeoutDuration returns the effective timeout as a time.Duration.
func TimeoutDuration(sec int) time.Duration {
	if sec <= 0 {
		return 30 * time.Second
	}
	return time.Duration(sec) * time.Second
}

type ShellConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	AllowedCommands   []string `mapstructure:"allowed_commands"`   // empty = allow all when allow_unrestricted is true
	AllowUnrestricted bool     `mapstructure:"allow_unrestricted"` // opt-in to unrestricted sh -c when no whitelist
	MaxCommandLength  int      `mapstructure:"max_command_length"` // 0 = use default
	TimeoutSeconds    int      `mapstructure:"timeout_seconds"`
	Priority          int      `mapstructure:"priority"`
}

type CDPConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Headless       bool   `mapstructure:"headless"`
	WsURL          string `mapstructure:"ws_url"`
	Priority       int    `mapstructure:"priority"`        // tool listing priority (lower = preferred)
	IdleTimeout    int    `mapstructure:"idle_timeout"`    // seconds, 0 = disabled
	StartupTimeout int    `mapstructure:"startup_timeout"` // seconds for browser init verify, 0 = use default 30s
}

// EmailMCPConfig controls the built-in email MCP tool.
type EmailMCPConfig struct {
	Enabled  bool `mapstructure:"enabled"`
	Priority int  `mapstructure:"priority"` // tool listing priority (lower = preferred)
}

type PoolConfig struct {
	MaxConcurrency      int    `mapstructure:"max_concurrency"`
	DefaultTimeout      int    `mapstructure:"default_timeout"`
	WorkspaceDir        string `mapstructure:"workspace_dir"`
	IdleTimeout         int    `mapstructure:"idle_timeout"`
	MaxPendingResults   int    `mapstructure:"max_pending_results"`
	MaxPendingResultLen int    `mapstructure:"max_pending_result_len"` // chars per result in prompt, 0 = no truncation
}

type SkillsConfig struct {
	Dir    string   `mapstructure:"dir"`     // skills directory (default: .dolphin/skills)
	MaxTop int      `mapstructure:"max_top"` // number of top skills to show in prompt (default: 10)
	Repos  []string `mapstructure:"repos"`   // manifest repos, e.g. ["dolphinv/skills"]
}

// AgentsConfig holds configuration for agent discovery.
type AgentsConfig struct {
	Repos []string `mapstructure:"repos"` // agent manifest repos, e.g. ["dolphinZzv/demo_agents"]
}

type PprofConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"` // listen address, e.g. ":6060"
}

type CrontabConfig struct {
	File          string `mapstructure:"file"`
	CheckInterval string `mapstructure:"check_interval"` // e.g. "30s"
}

type DiaryConfig struct {
	Dir            string `mapstructure:"dir"`
	MaxDaySessions int    `mapstructure:"max_day_sessions"`
	MaxWeekDays    int    `mapstructure:"max_week_days"`
	MaxMonthWeeks  int    `mapstructure:"max_month_weeks"`
	MaxYearMonths  int    `mapstructure:"max_year_months"`
	MaxTotalMB     int    `mapstructure:"max_total_mb"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"` // listen address, e.g. ":9090"
}

type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"` // listen address, e.g. ":9091"
}

// FlagsConfig controls optional feature flags.
type FlagsConfig struct {
	SelfEvolution bool `mapstructure:"self_evolution"` // enable self-evolution: BUILTIN_SKILLS.md + LLM CRUD tools for skills/commands
}

// ResourceConfig configures the system resource monitor.
type ResourceConfig struct {
	Enabled      bool      `mapstructure:"enabled"`       // enable periodic resource monitoring
	Interval     string    `mapstructure:"interval"`      // sampling interval, e.g. "30s" (default 30s)
	DiskPaths    []string  `mapstructure:"disk_paths"`    // filesystem paths to monitor (e.g. ["/", "/data"])
	MaxBandwidth uint64    `mapstructure:"max_bandwidth"` // max network bandwidth in bytes/sec for % calculation (default 125MB/s = 1Gbps)
	Thresholds   []float64 `mapstructure:"thresholds"`    // percentage thresholds to monitor, sorted ascending (default [20, 40, 60, 80])
}

// TelemetryConfig holds OpenTelemetry tracing configuration.
type TelemetryConfig struct {
	Enabled        bool              `mapstructure:"enabled"`
	ServiceName    string            `mapstructure:"service_name"`
	Exporter       string            `mapstructure:"exporter"` // otlp-grpc, otlp-http, stdout
	OTLPEndpoint   string            `mapstructure:"otlp_endpoint"`
	OTLPHeaders    map[string]string `mapstructure:"otlp_headers"`
	SampleRate     float64           `mapstructure:"sample_rate"`
	LogsEnabled    bool              `mapstructure:"logs_enabled"`
	MetricsEnabled bool              `mapstructure:"metrics_enabled"`
}

type UpdateConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	CheckInterval string `mapstructure:"check_interval"` // e.g. "24h", "12h", "1h"
	Channel       string `mapstructure:"channel"`        // "stable" or "pre-release"
	AutoInstall   bool   `mapstructure:"auto_install"`
}

type PluginsConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	Dir            string   `mapstructure:"dir"`             // script plugins directory
	WebhookURL     string   `mapstructure:"webhook_url"`     // HTTP POST events here
	WebhookEvents  []string `mapstructure:"webhook_events"`  // event types to send, ["*"] for all
	HeartbeatTurns int      `mapstructure:"heartbeat_turns"` // emit heartbeat every N turns, 0=off
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
	// Propagate env var or legacy api_key to providers with empty keys.
	// This lets users set DZ_LLM_API_KEY once and have it apply to all providers.
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
	// Propagate llm.max_tokens to providers that don't specify their own.
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
		_ = yaml.Unmarshal(data, &existing)
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

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Read existing config as full YAML map, merge tool selections, write back.
	// Read-modify-write avoids YAML corruption from blind append.
	full := make(map[string]any)
	if existingData, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(existingData, &full)
	}
	// Merge the merged loaded-tools into the full config.
	// Deep-merge into existing section maps to avoid overwriting other settings
	// like mcp.shell or skills.dir.
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
	// If multi-provider is configured, validate each entry.
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
		// Validate legacy single-provider fields.
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

func setDefaults(v *viper.Viper) {
	v.SetDefault("name", "dolphin")

	v.SetDefault("llm.type", "openai")
	v.SetDefault("llm.base_url", "https://api.openai.com/v1")
	v.SetDefault("llm.model", "gpt-4o")
	v.SetDefault("llm.max_tokens", 4096)
	v.SetDefault("llm.temperature", 0.7)
	v.SetDefault("llm.max_sub_turns", 10)
	v.SetDefault("llm.max_context_tokens", 1048576)
	v.SetDefault("llm.compress_mode", "drop")
	v.SetDefault("llm.segment_merge_limit", 100)

	v.SetDefault("session.max_loop", 50)
	v.SetDefault("session.summary", true)

	v.SetDefault("transport.stdio.enabled", true)
	v.SetDefault("transport.stdio.markdown_render", true)
	v.SetDefault("transport.stdio.markdown_style", "auto")
	v.SetDefault("transport.ssh.enabled", false)
	v.SetDefault("transport.ssh.markdown_render", false)
	v.SetDefault("transport.ssh.markdown_style", "auto")
	v.SetDefault("transport.ssh.addr", ":2222")
	v.SetDefault("transport.ssh.host_key", "~/.ssh/id_ed25519")
	v.SetDefault("transport.ssh.username", "dolphin")
	v.SetDefault("transport.ssh.password", "")
	v.SetDefault("transport.mqtt.enabled", false)
	v.SetDefault("transport.mqtt.broker", "tcp://localhost:1883")
	v.SetDefault("transport.mqtt.subscribe_topic", "/agent/dolphin")
	v.SetDefault("transport.mqtt.publish_topic", "/agent/dolphin/message")
	v.SetDefault("transport.mqtt.client_id", "dolphin-agent")

	v.SetDefault("servers.mqtt_broker.enabled", true)
	v.SetDefault("servers.mqtt_broker.addr", ":1883")

	v.SetDefault("transport.email.enabled", false)
	v.SetDefault("transport.email.smtp_port", 587)
	v.SetDefault("transport.email.imap_port", 993)
	v.SetDefault("transport.email.use_tls", true)
	v.SetDefault("transport.email.poll_interval", "10s")
	v.SetDefault("transport.email.allowed_senders", []string{})

	v.SetDefault("transport.dingtalk.enabled", false)

	v.SetDefault("transport.acp.enabled", false)
	v.SetDefault("transport.acp.listen_addr", ":8333")
	v.SetDefault("transport.acp.agent_id", "dolphin")
	v.SetDefault("transport.acp.agent_name", "Dolphin AI Agent")
	v.SetDefault("transport.acp.agent_version", "0.1.0")
	v.SetDefault("transport.acp.agent_description", "Cross-terminal/email/chat/SSH AI agent")
	v.SetDefault("transport.acp.capabilities", []string{"task-execution", "shell-command", "web-search"})
	v.SetDefault("transport.acp.sync_timeout", "60s")
	v.SetDefault("transport.acp.api_key", "")
	v.SetDefault("transport.acp.tls_enabled", false)
	v.SetDefault("transport.acp.tls_cert_file", "")
	v.SetDefault("transport.acp.tls_key_file", "")

	v.SetDefault("transport.a2a.enabled", false)
	v.SetDefault("transport.a2a.listen_addr", ":8334")
	v.SetDefault("transport.a2a.agent_id", "dolphin")
	v.SetDefault("transport.a2a.agent_name", "Dolphin AI Agent")
	v.SetDefault("transport.a2a.agent_version", "0.1.0")
	v.SetDefault("transport.a2a.agent_description", "Cross-terminal/email/chat/SSH AI agent")
	v.SetDefault("transport.a2a.capabilities", []string{"task-execution", "shell-command", "web-search"})
	v.SetDefault("transport.a2a.sync_timeout", "60s")
	v.SetDefault("transport.a2a.api_key", "")
	v.SetDefault("transport.a2a.tls_enabled", false)
	v.SetDefault("transport.a2a.tls_cert_file", "")
	v.SetDefault("transport.a2a.tls_key_file", "")

	v.SetDefault("session.max_age", "24h")
	v.SetDefault("session.resume", false)

	v.SetDefault("mcp.shell.enabled", true)
	v.SetDefault("mcp.shell.allow_unrestricted", true)
	v.SetDefault("mcp.shell.timeout_seconds", 30)
	v.SetDefault("mcp.shell.priority", 10)
	v.SetDefault("mcp.shell.max_command_length", 4096)
	v.SetDefault("mcp.cdp.enabled", true)
	v.SetDefault("mcp.cdp.headless", true)
	v.SetDefault("mcp.cdp.priority", 1000)
	v.SetDefault("mcp.cdp.idle_timeout", 300)
	v.SetDefault("mcp.cdp.startup_timeout", 30)
	v.SetDefault("mcp.email.enabled", true)
	v.SetDefault("mcp.email.priority", 500)

	v.SetDefault("mcp.webhook.enabled", true)
	v.SetDefault("mcp.webhook.priority", 100)

	v.SetDefault("mcp.web_search.enabled", true)
	v.SetDefault("mcp.web_search.priority", 90)
	v.SetDefault("mcp.web_search.provider", "duckduckgo")
	v.SetDefault("mcp.web_search.api_key", "")

	v.SetDefault("agent_pool.max_concurrency", 5)
	v.SetDefault("agent_pool.default_timeout", 300)
	v.SetDefault("agent_pool.workspace_dir", ".dolphin/workspaces")
	v.SetDefault("agent_pool.idle_timeout", 600)
	v.SetDefault("agent_pool.max_pending_results", 10)

	v.SetDefault("skills.dir", ".dolphin/skills")
	v.SetDefault("skills.max_top", 10)
	v.SetDefault("skills.repos", []string{})

	v.SetDefault("crontab.file", ".dolphin/CRONTAB.md")
	v.SetDefault("crontab.check_interval", "30s")

	v.SetDefault("pprof.enabled", false)
	v.SetDefault("pprof.addr", "127.0.0.1:6060")

	v.SetDefault("diary.dir", ".dolphin/diary")
	v.SetDefault("diary.max_day_sessions", 200)
	v.SetDefault("diary.max_week_days", 7)
	v.SetDefault("diary.max_month_weeks", 5)
	v.SetDefault("diary.max_year_months", 12)
	v.SetDefault("diary.max_total_mb", 500)

	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.addr", "127.0.0.1:9090")

	v.SetDefault("health.enabled", false)
	v.SetDefault("health.addr", "127.0.0.1:9091")

	v.SetDefault("telemetry.enabled", false)
	v.SetDefault("telemetry.service_name", "dolphin")
	v.SetDefault("telemetry.exporter", "stdout")
	v.SetDefault("telemetry.otlp_endpoint", "localhost:4317")
	v.SetDefault("telemetry.otlp_headers", map[string]string{})
	v.SetDefault("telemetry.sample_rate", 1.0)
	v.SetDefault("telemetry.logs_enabled", false)
	v.SetDefault("telemetry.metrics_enabled", false)

	v.SetDefault("flags.self_evolution", false)

	v.SetDefault("resource.enabled", false)
	v.SetDefault("resource.interval", "30s")
	v.SetDefault("resource.disk_paths", []string{"/"})
	v.SetDefault("resource.max_bandwidth", 125000000)
	v.SetDefault("resource.thresholds", []float64{20, 40, 60, 80})

	v.SetDefault("sync_config", true)

	v.SetDefault("update.enabled", true)
	v.SetDefault("update.check_interval", "24h")
	v.SetDefault("update.channel", "stable")
	v.SetDefault("update.auto_install", false)

	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", ".dolphin/logs/agent.log")
	v.SetDefault("log_max_size", 100)
	v.SetDefault("log_max_age", 30)
	v.SetDefault("log_max_backup", 3)

	v.SetDefault("plugins.enabled", true)
	v.SetDefault("plugins.dir", "~/.dolphin/plugins/")
	v.SetDefault("plugins.webhook_url", "")
	v.SetDefault("plugins.heartbeat_turns", 0)
	v.SetDefault("plugins.webhook_events", []string{"*"})
}
