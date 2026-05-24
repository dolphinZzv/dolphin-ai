package config

import (
	"fmt"
	"time"
)

// ProviderConfig defines a single LLM provider endpoint.
type ProviderConfig struct {
	Name      string `mapstructure:"name"`
	Type      string `mapstructure:"type"` // "openai" or "anthropic"
	BaseURL   string `mapstructure:"base_url"`
	APIKey    string `mapstructure:"api_key"`
	Model     string `mapstructure:"model"`
	MaxTokens int    `mapstructure:"max_tokens"`

	// TimeoutSeconds overrides the global llm.timeout_seconds for this provider, 0 = use global default.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
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

	// TimeoutSeconds sets the HTTP client timeout per provider, 0 = default 5min.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// HealthCheckTimeoutSeconds controls the per-provider health check timeout, 0 = default 10s.
	HealthCheckTimeoutSeconds int `mapstructure:"health_check_timeout_seconds"`

	// CompressTimeoutSeconds sets the per-LLM-call timeout for compression/summary, 0 = default 15s.
	CompressTimeoutSeconds int `mapstructure:"compress_timeout_seconds"`

	// Retry configures LLM API call retry behavior (transient failures, not rate limits).
	Retry LLMRetryConfig `mapstructure:"retry"`

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
	MaxLoop   int    `mapstructure:"max_loop"`
	Summary   bool   `mapstructure:"summary"`
	MaxAge    string `mapstructure:"max_age"`
	Resume    bool   `mapstructure:"resume"`
	MaxSizeMB int    `mapstructure:"max_size_mb"` // max session file size in MB, 0 = default 10
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

func (c *LimitsConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Enforcement != "hard" && c.Enforcement != "soft" && c.Enforcement != "" {
		return fmt.Errorf("enforcement must be 'hard' or 'soft', got '%s'", c.Enforcement)
	}

	if c.Enforcement == "soft" && c.Retry.MaxAttempts == 0 {
		return fmt.Errorf("soft enforcement requires retry.max_attempts > 0")
	}

	return nil
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

// LLMRetryConfig controls retry behavior for transient LLM API failures.
type LLMRetryConfig struct {
	MaxAttempts int    `mapstructure:"max_attempts"` // per-provider retry count, 0 = default 3
	BackoffBase string `mapstructure:"backoff_base"` // exponential backoff base, e.g. "1s"; empty = default 1s
}

type CredentialsConfig struct {
	Enabled    bool     `mapstructure:"enabled"`
	Store      string   `mapstructure:"store"`
	Path       string   `mapstructure:"path"`
	SafeFields []string `mapstructure:"safe_fields"`
	AllowOnly  []string `mapstructure:"allow_only"`
}

type TransportConfig struct {
	Stdio    StdioConfig    `mapstructure:"stdio"`
	SSH      SSHConfig      `mapstructure:"ssh"`
	MQTT     MQTTConfig     `mapstructure:"mqtt"`
	Email    EmailConfig    `mapstructure:"email"`
	DingTalk DingTalkConfig `mapstructure:"dingtalk"`
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

// A2AConfig holds configuration for the A2A (Agent-to-Agent) transport.
// Uses JSON-RPC 2.0 over HTTP, following Google A2A specification.
type A2AConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	ListenAddr        string   `mapstructure:"listen_addr"`
	AgentID           string   `mapstructure:"agent_id"`
	AgentName         string   `mapstructure:"agent_name"`
	AgentVersion      string   `mapstructure:"agent_version"`
	AgentDesc         string   `mapstructure:"agent_description"`
	Capabilities      []string `mapstructure:"capabilities"`
	SyncTimeout       string   `mapstructure:"sync_timeout"`
	APIKey            string   `mapstructure:"api_key"`
	TLSEnabled        bool     `mapstructure:"tls_enabled"`
	TLSCertFile       string   `mapstructure:"tls_cert_file"`
	TLSKeyFile        string   `mapstructure:"tls_key_file"`
	HandlerPath       string   `mapstructure:"handler_path"`        // HTTP handler path for A2A RPC, default "/a2a"
	AgentCardPath     string   `mapstructure:"agent_card_path"`     // Agent Card endpoint, default "/.well-known/agent.json"
	ReadHeaderTimeout int      `mapstructure:"read_header_timeout"` // HTTP server ReadHeaderTimeout in seconds, 0 = default 10s
	ShutdownTimeout   int      `mapstructure:"shutdown_timeout"`    // server Shutdown context timeout in seconds, 0 = default 5s
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
	ReadTimeout    string `mapstructure:"read_timeout"` // ReadLine deadline, e.g. "5m"; empty = default 5m
}

type MQTTAccount struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type MQTTConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	Broker              string `mapstructure:"broker"`
	SubscribeTopic      string `mapstructure:"subscribe_topic"`
	PublishTopic        string `mapstructure:"publish_topic"`
	ClientID            string `mapstructure:"client_id"`
	Username            string `mapstructure:"username"`
	Password            string `mapstructure:"password"`
	KeepAliveSeconds    int    `mapstructure:"keep_alive_seconds"`    // MQTT KeepAlive, 0 = default 60
	PingTimeoutSeconds  int    `mapstructure:"ping_timeout_seconds"`  // MQTT ping timeout, 0 = default 10
	MaxReconnectSeconds int    `mapstructure:"max_reconnect_seconds"` // MQTT max reconnect interval, 0 = default 30
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
	DialTimeout    string   `mapstructure:"dial_timeout"`    // IMAP/POP3 dial timeout, e.g. "30s"; empty = default 30s
}

// DingTalkConfig holds configuration for the DingTalk bot transport.
// Uses Stream mode (WebSocket long connection) — no public IP or callback URL needed.
// The bot actively connects to DingTalk servers and receives messages via push.
type DingTalkConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	ClientID     string `mapstructure:"client_id"`     // AppKey from DingTalk Open Platform
	ClientSecret string `mapstructure:"client_secret"` // AppSecret from DingTalk Open Platform
	ReadTimeout  string `mapstructure:"read_timeout"`  // ReadLine timeout, e.g. "5m"; empty = default 5m
}

type MCPConfig struct {
	Shell       ShellConfig                `mapstructure:"shell"`
	CDP         CDPConfig                  `mapstructure:"cdp"`
	Email       EmailMCPConfig             `mapstructure:"email"`
	Webhook     MCPWebhookConfig           `mapstructure:"webhook"`
	WebSearch   MCPWebSearchConfig         `mapstructure:"web_search"`
	Credentials CredentialsConfig          `mapstructure:"credentials"`
	A2A         MCPA2AConfig               `mapstructure:"a2a"`
	Servers     map[string]MCPServerConfig `mapstructure:"servers"`
	Repos       []string                   `mapstructure:"repos"`
}

type MCPWebhookConfig struct {
	Enabled        bool                     `mapstructure:"enabled"`
	Priority       int                      `mapstructure:"priority"`
	TimeoutSeconds int                      `mapstructure:"timeout_seconds"` // HTTP client timeout, 0 = use default 30s
	Targets        map[string]WebhookTarget `mapstructure:"targets"`         // named pre-configured webhook targets
}

type WebhookTarget struct {
	URL     string            `mapstructure:"url"`
	Method  string            `mapstructure:"method"`  // HTTP method, e.g. "POST" (default), "GET"
	Headers map[string]string `mapstructure:"headers"` // custom HTTP headers
}

type MCPWebSearchConfig struct {
	Enabled          bool              `mapstructure:"enabled"`
	Priority         int               `mapstructure:"priority"`
	Provider         string            `mapstructure:"provider"`
	Providers        []string          `mapstructure:"providers"`
	APIKey           string            `mapstructure:"api_key"`
	TimeoutSeconds   int               `mapstructure:"timeout_seconds"`    // HTTP client timeout, 0 = use default 15s
	UserAgent        string            `mapstructure:"user_agent"`         // User-Agent for provider requests; empty = provider default
	MaxResults       int               `mapstructure:"max_results"`        // max results per provider query, 0 = provider default
	ProviderBaseURLs map[string]string `mapstructure:"provider_base_urls"` // per-provider base URL override, e.g. duckduckgo: https://html.duckduckgo.com/html/
}

type MCPA2AConfig struct {
	Enabled        bool             `mapstructure:"enabled"`
	TimeoutSeconds int              `mapstructure:"timeout_seconds"`  // HTTP client timeout, 0 = use default 30s
	DefaultRPCPath string           `mapstructure:"default_rpc_path"` // RPC endpoint path suffix, default "/rpc"
	Agents         []A2AAgentConfig `mapstructure:"agents"`
}

type A2AAgentConfig struct {
	Name   string `mapstructure:"name"`
	URL    string `mapstructure:"url"`
	APIKey string `mapstructure:"api_key"`
}

type MCPServerConfig struct {
	Type            string            `mapstructure:"type"`             // "stdio", "sse", "http-stream"
	Command         string            `mapstructure:"command"`          // for stdio type
	Args            []string          `mapstructure:"args"`             // for stdio type
	URL             string            `mapstructure:"url"`              // for sse / http-stream type
	Headers         map[string]string `mapstructure:"headers"`          // custom HTTP headers (auth etc.)
	Timeout         int               `mapstructure:"timeout"`          // request timeout in seconds, 0 = default 30
	Enabled         *bool             `mapstructure:"enabled"`          // nil or true = enabled, false = skip
	ReconnectDelay  string            `mapstructure:"reconnect_delay"`  // SSE reconnect delay, e.g. "5s"; empty = default 5s
	ShutdownTimeout int               `mapstructure:"shutdown_timeout"` // stdio shutdown grace period in seconds, 0 = default 3s
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
	AllowedCommands   []string `mapstructure:"allowed_commands"`   // default: ["date"]; empty + allow_unrestricted=true = allow all
	AllowUnrestricted bool     `mapstructure:"allow_unrestricted"` // opt-in to unrestricted sh -c when no whitelist
	MaxCommandLength  int      `mapstructure:"max_command_length"` // 0 = use default 4096
	TimeoutSeconds    int      `mapstructure:"timeout_seconds"`
	Priority          int      `mapstructure:"priority"`
	OutputMaxBytes    int      `mapstructure:"output_max_bytes"` // stdout/stderr truncation limit, 0 = use default 64KB
}

type CDPConfig struct {
	Enabled            bool           `mapstructure:"enabled"`
	Headless           bool           `mapstructure:"headless"`
	WsURL              string         `mapstructure:"ws_url"`
	Priority           int            `mapstructure:"priority"`             // tool listing priority (lower = preferred)
	IdleTimeout        int            `mapstructure:"idle_timeout"`         // seconds, 0 = disabled
	StartupTimeout     int            `mapstructure:"startup_timeout"`      // seconds for browser init verify, 0 = use default 30s
	ChromeFlags        map[string]any `mapstructure:"chrome_flags"`         // additional chromedp flags, overrides built-in defaults
	UserAgent          string         `mapstructure:"user_agent"`           // custom User-Agent string; empty = use default
	HealthCheckTimeout int            `mapstructure:"health_check_timeout"` // seconds for browser health check, 0 = use default 10s
	NavigationWait     string         `mapstructure:"navigation_wait"`      // post-navigation wait duration, e.g. "2s"; empty = no extra wait
	ScreenshotQuality  int            `mapstructure:"screenshot_quality"`   // full page screenshot quality 0-100, 0 = use default 100
	ScreenshotDir      string         `mapstructure:"screenshot_dir"`       // screenshots output directory; empty = use default "screenshots/"
}

// EmailMCPConfig controls the built-in email MCP tool.
type EmailMCPConfig struct {
	Enabled           bool   `mapstructure:"enabled"`
	Priority          int    `mapstructure:"priority"`            // tool listing priority (lower = preferred)
	MaxAttachmentSize int    `mapstructure:"max_attachment_size"` // bytes, 0 = use default 10MB
	ConnectTimeout    string `mapstructure:"connect_timeout"`     // IMAP/POP3 connection timeout, e.g. "30s"; empty = default 30s
}

type PoolConfig struct {
	MaxConcurrency      int    `mapstructure:"max_concurrency"`
	DefaultTimeout      int    `mapstructure:"default_timeout"`
	WorkspaceDir        string `mapstructure:"workspace_dir"`
	IdleTimeout         int    `mapstructure:"idle_timeout"`
	MaxPendingResults   int    `mapstructure:"max_pending_results"`
	MaxPendingResultLen int    `mapstructure:"max_pending_result_len"` // chars per result in prompt, 0 = no truncation
	MaxSynthesisRounds  int    `mapstructure:"max_synthesis_rounds"`   // cap on coordinator poll synthesis, 0 = default 3
	PollInterval        string `mapstructure:"poll_interval"`          // sub-agent ready poll interval, e.g. "200ms"; empty = default 200ms
	MinReapInterval     string `mapstructure:"min_reap_interval"`      // idle reap minimum interval, e.g. "5s"; empty = default 5s
	MaxReapInterval     string `mapstructure:"max_reap_interval"`      // idle reap maximum interval, e.g. "30s"; empty = default 30s
}

type SkillsConfig struct {
	Dir    string   `mapstructure:"dir"`     // skills directory (default: .dolphin/skills)
	MaxTop int      `mapstructure:"max_top"` // number of top skills to show in prompt (default: 10)
	Repos  []string `mapstructure:"repos"`   // manifest repos, e.g. ["dolphinv/skills"]
}

// WorkflowsConfig holds configuration for workflow definitions.
type WorkflowsConfig struct {
	Dir string `mapstructure:"dir"` // workflows directory (default: .dolphin/workflows)
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
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`     // listen address, e.g. ":9091"
	Debounce string `mapstructure:"debounce"` // heartbeat debounce interval, e.g. "30s"; empty = default 30s
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
	Enabled        bool   `mapstructure:"enabled"`
	CheckInterval  string `mapstructure:"check_interval"` // e.g. "24h", "12h", "1h"
	Channel        string `mapstructure:"channel"`        // "stable" or "pre-release"
	AutoInstall    bool   `mapstructure:"auto_install"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"` // HTTP client timeout, 0 = default 30s
}

type PluginsConfig struct {
	Enabled              bool     `mapstructure:"enabled"`
	Dir                  string   `mapstructure:"dir"`                    // script plugins directory
	WebhookURL           string   `mapstructure:"webhook_url"`            // HTTP POST events here
	WebhookEvents        []string `mapstructure:"webhook_events"`         // event types to send, ["*"] for all
	HeartbeatTurns       int      `mapstructure:"heartbeat_turns"`        // emit heartbeat every N turns, 0=off
	ScriptTimeoutSeconds int      `mapstructure:"script_timeout_seconds"` // per-script execution timeout, 0 = default 3s
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
