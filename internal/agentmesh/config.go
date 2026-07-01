package agentmesh

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"dolphin/internal/config"
)

// AgentConfig is the runtime configuration for an AgentMesh instance.
type AgentConfig struct {
	Enabled            bool
	Name               string
	ListenAddr         string
	Capabilities       []string
	TaskTimeout        time.Duration
	MaxDelegationDepth int

	Local  []RemoteAgent
	Remote []RemoteAgent

	Retry           RetryConfig
	Fallback        FallbackConfig
	CircuitBreaker  CircuitBreakerConfig
	RateLimit       RateLimitConfig
	ServerRateLimit ServerRateLimitConfig
	Spawner         SpawnerConfig
	GossipConfig    GossipConfig
	TLS             TLSConfig
}

// ServerRateLimitConfig configures receiver-side rate limiting.
type ServerRateLimitConfig struct {
	SessionPerMin int // requests per minute per parent session, default 30
	PeerPerMin    int // requests per minute per upstream agent, default 60
	GlobalPerMin  int // requests per minute global, default 120
}

// RetryConfig controls retry behaviour on retryable failures.
type RetryConfig struct {
	MaxRetries int
	Backoff    time.Duration
	MaxBackoff time.Duration
	RetryOn    []ErrorCode
}

// FallbackConfig controls fallback to alternative agents.
type FallbackConfig struct {
	Enabled     bool
	MaxFallback int
}

// CircuitBreakerConfig configures the per-agent circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int
	CooldownPeriod   time.Duration
	HalfOpenMax      int
}

// RateLimitConfig configures sender-side rate limiting.
type RateLimitConfig struct {
	SendPerAgent float64 // requests per second per target agent
	SendBurst    int     // burst size
}

// SpawnerConfig is reserved for Phase 2 (dynamic child-process spawning).
type SpawnerConfig struct {
	Enabled bool
	Bin     string
	MaxSpawned int
}

// TLSConfig configures mutual TLS for A2A client connections (Phase 6).
type TLSConfig struct {
	Enabled            bool
	CACert             string // path to CA cert
	ClientCert         string // path to client cert
	ClientKey          string // path to client key
	InsecureSkipVerify bool   // testing only
}

// Build assembles a *crypto/tls.Config from file paths.
func (t *TLSConfig) Build() (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: t.InsecureSkipVerify}
	if t.CACert != "" {
		caData, err := os.ReadFile(t.CACert)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("invalid ca cert: %s", t.CACert)
		}
		cfg.RootCAs = pool
	}
	if t.ClientCert != "" && t.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(t.ClientCert, t.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client keypair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// DefaultAgentConfig returns production defaults.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Enabled:            false,
		ListenAddr:         ":8100",
		TaskTimeout:        10 * time.Minute,
		MaxDelegationDepth: 5,
		Retry: RetryConfig{
			MaxRetries: 2,
			Backoff:    1 * time.Second,
			MaxBackoff: 30 * time.Second,
			RetryOn:    []ErrorCode{ErrTimeout, ErrAgentUnavail, ErrAgentBusy},
		},
		Fallback: FallbackConfig{
			Enabled:     true,
			MaxFallback: 2,
		},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold: 5,
			CooldownPeriod:   60 * time.Second,
			HalfOpenMax:      1,
		},
		RateLimit: RateLimitConfig{
			SendPerAgent: 2,
			SendBurst:    5,
		},
		ServerRateLimit: ServerRateLimitConfig{
			SessionPerMin: 30,
			PeerPerMin:    60,
			GlobalPerMin:  120,
		},
		Spawner: SpawnerConfig{Enabled: false, MaxSpawned: 5},
		GossipConfig: DefaultGossipConfig(),
	}
}

// LoadAgentConfig reads the `agents.` prefix from a config.Config, falling
// back to defaults for missing keys. This keeps AgentMesh configuration
// consistent with the rest of Dolphin's dot-notation config.
func LoadAgentConfig(cfg *config.Config) AgentConfig {
	if cfg == nil {
		return DefaultAgentConfig()
	}
	ac := DefaultAgentConfig()
	if cfg.Get("agents.enabled") != nil {
		ac.Enabled = cfg.GetBool("agents.enabled")
	}
	if v := cfg.GetString("agents.listen_addr"); v != "" {
		ac.ListenAddr = v
	}
	if v := cfg.GetString("agents.name"); v != "" {
		ac.Name = v
	}
	if v := cfg.GetDuration("agents.task_timeout"); v > 0 {
		ac.TaskTimeout = v
	}
	if v := cfg.GetInt("agents.max_delegation_depth"); v > 0 {
		ac.MaxDelegationDepth = v
	}
	if v := cfg.GetInt("agents.retry.max_retries"); v > 0 {
		ac.Retry.MaxRetries = v
	}
	if v := cfg.GetDuration("agents.retry.backoff"); v > 0 {
		ac.Retry.Backoff = v
	}
	if v := cfg.GetDuration("agents.retry.max_backoff"); v > 0 {
		ac.Retry.MaxBackoff = v
	}
	if v := cfg.GetInt("agents.fallback.max_fallback"); v > 0 {
		ac.Fallback.MaxFallback = v
	}
	if cfg.Get("agents.fallback.enabled") != nil {
		ac.Fallback.Enabled = cfg.GetBool("agents.fallback.enabled")
	}
	if v := cfg.GetInt("agents.circuit_breaker.failure_threshold"); v > 0 {
		ac.CircuitBreaker.FailureThreshold = v
	}
	if v := cfg.GetDuration("agents.circuit_breaker.cooldown_period"); v > 0 {
		ac.CircuitBreaker.CooldownPeriod = v
	}
	if v := cfg.GetInt("agents.rate_limit.send_burst"); v > 0 {
		ac.RateLimit.SendBurst = v
	}
	if v := cfg.GetInt("agents.spawner.max_spawned"); v > 0 {
		ac.Spawner.MaxSpawned = v
	}
	if cfg.Get("agents.spawner.enabled") != nil {
		ac.Spawner.Enabled = cfg.GetBool("agents.spawner.enabled")
	}
	if v := cfg.GetString("agents.spawner.bin"); v != "" {
		ac.Spawner.Bin = v
	}
	// server rate limit
	if v := cfg.GetInt("agents.server_rate_limit.session_per_min"); v > 0 {
		ac.ServerRateLimit.SessionPerMin = v
	}
	if v := cfg.GetInt("agents.server_rate_limit.peer_per_min"); v > 0 {
		ac.ServerRateLimit.PeerPerMin = v
	}
	if v := cfg.GetInt("agents.server_rate_limit.global_per_min"); v > 0 {
		ac.ServerRateLimit.GlobalPerMin = v
	}
	// gossip
	if cfg.Get("agents.gossip.enabled") != nil {
		ac.GossipConfig.Enabled = cfg.GetBool("agents.gossip.enabled")
	}
	if v := cfg.GetInt("agents.gossip.port"); v > 0 {
		ac.GossipConfig.Port = v
	}
	if v := cfg.GetDuration("agents.gossip.announce_interval"); v > 0 {
		ac.GossipConfig.AnnounceInterval = v
	}
	if v := cfg.GetDuration("agents.gossip.peer_timeout"); v > 0 {
		ac.GossipConfig.PeerTimeout = v
	}
	if v := cfg.GetInt("agents.gossip.max_hops"); v > 0 {
		ac.GossipConfig.MaxHops = v
	}
	ac.Local = loadRemoteAgents(cfg, "agents.local")
	ac.Remote = loadRemoteAgents(cfg, "agents.remote")
	// TLS
	if cfg.Get("agents.tls.enabled") != nil {
		ac.TLS.Enabled = cfg.GetBool("agents.tls.enabled")
	}
	ac.TLS.CACert = cfg.GetString("agents.tls.ca_cert")
	ac.TLS.ClientCert = cfg.GetString("agents.tls.client_cert")
	ac.TLS.ClientKey = cfg.GetString("agents.tls.client_key")
	if cfg.Get("agents.tls.insecure_skip_verify") != nil {
		ac.TLS.InsecureSkipVerify = cfg.GetBool("agents.tls.insecure_skip_verify")
	}
	return ac
}

// loadRemoteAgents reads a `agents.local`/`agents.remote` list from config.
// The dot-notation Config does not natively expose typed slices, so we read
// the underlying values map.
func loadRemoteAgents(cfg *config.Config, prefix string) []RemoteAgent {
	out := []RemoteAgent{}
	keys := cfg.Keys()
	// Collect indices present under prefix.
	indices := map[int]bool{}
	for _, k := range keys {
		if len(k) <= len(prefix)+1 || k[:len(prefix)+1] != prefix+"." {
			continue
		}
		rest := k[len(prefix)+1:] // e.g. "0.name"
		dot := indexByte(rest, '.')
		if dot < 0 {
			continue
		}
		var idx int
		for _, ch := range rest[:dot] {
			if ch < '0' || ch > '9' {
				idx = -1
				break
			}
			idx = idx*10 + int(ch-'0')
		}
		if idx >= 0 {
			indices[idx] = true
		}
	}
	for idx := range indices {
		out = append(out, RemoteAgent{
			Name:         cfg.GetString(prefix + "." + itoa(idx) + ".name"),
			Addr:         cfg.GetString(prefix + "." + itoa(idx) + ".addr"),
			Model:        cfg.GetString(prefix + "." + itoa(idx) + ".model"),
		})
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
