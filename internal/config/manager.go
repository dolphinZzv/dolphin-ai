package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Subscriber receives notifications when the config changes.
// Components should use reflect.DeepEqual to diff oldCfg vs newCfg
// and only react to relevant sections.
type Subscriber interface {
	OnConfigChange(oldCfg, newCfg *Config)
}

// Manager wraps a *Config with thread-safe access and reload support.
type Manager struct {
	mu          sync.RWMutex
	config      *Config
	enabled     bool
	loaded      bool // true after first successful Load()
	subscribers []Subscriber
	cfgFile     string // path to the CLI -c flag value, if any
}

// NewManager creates a Manager. The cfgFile parameter is the path from the -c
// CLI flag, if any.
func NewManager(cfgFile string) *Manager {
	return &Manager{
		enabled: true, // default: enabled, overridden after first Load()
		cfgFile: cfgFile,
	}
}

// Get returns the current Config snapshot. The returned pointer is safe to read
// but must not be mutated. For mutation, use Clone().
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Subscribe registers a subscriber for config change events.
func (m *Manager) Subscribe(s Subscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, s)
}

// Load reads all config files (system, user, project, -c flag), applies env var
// overrides and post-processing, validates, and atomically swaps the active
// config. On subsequent calls (reloads), it emits events for subscribers.
func (m *Manager) Load() error {
	newCfg, err := loadConfigFromFiles(m.cfgFile)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	// syncConfigDefaults only on first load to avoid a write→watch→reload loop.
	m.mu.RLock()
	first := !m.loaded
	m.mu.RUnlock()
	if first && newCfg.SyncConfig {
		if err := fillConfigDefaults(); err != nil {
			zap.S().Warnw("failed to sync config defaults", "error", err)
		}
	}

	return m.swap(newCfg)
}

// swap atomically replaces the active config and emits change events.
func (m *Manager) swap(newCfg *Config) error {
	m.mu.Lock()
	oldCfg := m.config
	m.config = newCfg
	m.enabled = newCfg.DynamicConfig.Enabled
	first := !m.loaded
	if !m.loaded {
		m.loaded = true
	}
	m.mu.Unlock()

	if oldCfg == nil {
		return nil // first load, no diff needed
	}

	if first {
		// On first load, the subscribers aren't wired yet — but the MCP config
		// tool may have mutated m.config via the agent's pointer. Still emit so
		// any subscriber registered after first load can catch up.
	}

	m.emitEvents(oldCfg, newCfg)
	return nil
}

// emitEvents notifies all subscribers with old and new config.
func (m *Manager) emitEvents(oldCfg, newCfg *Config) {
	if reflect.DeepEqual(oldCfg, newCfg) {
		zap.S().Debugw("config reload: no changes detected")
		return
	}

	m.mu.RLock()
	subs := make([]Subscriber, len(m.subscribers))
	copy(subs, m.subscribers)
	m.mu.RUnlock()

	zap.S().Infow("config reloaded, notifying subscribers", "count", len(subs))
	for _, sub := range subs {
		sub.OnConfigChange(oldCfg, newCfg)
	}
}

// Watch blocks until ctx is cancelled. If the manager is disabled, it blocks
// without watching. Otherwise it watches configured sources and triggers
// Load() on change.
func (m *Manager) Watch(ctx context.Context) error {
	if !m.enabled {
		zap.S().Infow("dynamic config disabled, skipping file watcher")
		<-ctx.Done()
		return nil
	}

	// Determine which config files to watch.
	paths := m.configFilePaths()
	if len(paths) == 0 {
		zap.S().Warnw("no config files to watch")
		<-ctx.Done()
		return nil
	}

	zap.S().Infow("watching config files for changes", "paths", paths)

	// Watch each config file with its own FileSource, running in parallel.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan error, len(paths))
	for _, p := range paths {
		p := p
		src := NewFileSource(p)
		go func() {
			results <- src.Watch(ctx, func() {
				if err := m.Load(); err != nil {
					zap.S().Errorw("config reload failed", "path", p, "error", err)
				} else {
					zap.S().Infow("config reloaded from file change", "path", p)
				}
			})
		}()
	}

	// Wait for all watchers to finish or context to be cancelled.
	var firstErr error
	for range paths {
		if err := <-results; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// configFilePaths returns the paths of config files that can be watched.
// The order is: project config, -c flag config, and user config.
// Non-existent files are excluded.
func (m *Manager) configFilePaths() []string {
	var paths []string

	// Project config: .dolphin/config.yaml
	projectCfg := filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")
	if _, err := os.Stat(projectCfg); err == nil {
		paths = append(paths, projectCfg)
	}

	// -c flag override
	if m.cfgFile != "" {
		if _, err := os.Stat(m.cfgFile); err == nil {
			paths = append(paths, m.cfgFile)
		}
	}

	// User config: ~/.dolphin/config.yaml
	if homeDir, err := os.UserHomeDir(); err == nil {
		userCfg := filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml")
		if _, err := os.Stat(userCfg); err == nil {
			paths = append(paths, userCfg)
		}
	}

	// Deduplicate
	seen := make(map[string]bool, len(paths))
	uniq := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, _ := filepath.Abs(p)
		if !seen[abs] {
			seen[abs] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// Enabled returns whether dynamic reload is enabled.
func (m *Manager) Enabled() bool { return m.enabled }

// ---- file loading ----

// loadConfigFromFiles replicates the config.Load() file merging logic but returns
// a fresh Config without calling fillConfigDefaults (caller gates that).
func loadConfigFromFiles(cfgFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	setDefaults(v)

	var configFiles []string

	// 1. System config
	configFiles = append(configFiles, filepath.Join(SystemConfigDir, ConfigFileName+".yaml"))
	// 2. User config
	if homeDir, err := os.UserHomeDir(); err == nil {
		configFiles = append(configFiles, filepath.Join(homeDir, UserConfigDir, ConfigFileName+".yaml"))
	}
	// 3. Project config
	configFiles = append(configFiles, filepath.Join(ProjectConfigDir, ConfigFileName+".yaml"))
	// 4. -c flag
	if cfgFile != "" {
		configFiles = append(configFiles, cfgFile)
	}

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

	v.SetEnvPrefix("DZ")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Manual env var overrides
	applyEnvOverrides(&cfg)

	// Resolve workspace to absolute path
	if cfg.Workspace == "" {
		cfg.Workspace = "."
	}
	if abs, err := filepath.Abs(cfg.Workspace); err == nil {
		cfg.Workspace = abs
	}

	// Auto-generate MQTT broker account
	autoGenMQTTAccount(&cfg)

	// Wire MQTT broker into transport
	wireMQTTBroker(&cfg)

	// Auto-generate SSH password
	ensureSSHPassword(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

// applyEnvOverrides applies DZ_* environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
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
		cfg.Log.Level = v
	}
	if v := os.Getenv("DZ_LOG_FILE"); v != "" {
		cfg.Log.File = v
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
	if v := os.Getenv("DZ_WORKSPACE"); v != "" {
		cfg.Workspace = v
	}
	if v := os.Getenv("DZ_LANGUAGE"); v != "" {
		cfg.Language = v
	}
}

func autoGenMQTTAccount(cfg *Config) {
	if cfg.Servers.MQTTBroker.Enabled && len(cfg.Servers.MQTTBroker.Accounts) == 0 {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err == nil {
			cfg.Servers.MQTTBroker.Accounts = []MQTTAccount{{
				Username: "dolphin",
				Password: hex.EncodeToString(buf),
			}}
		}
	}
}

func wireMQTTBroker(cfg *Config) {
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
}

func ensureSSHPassword(cfg *Config) {
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
}
