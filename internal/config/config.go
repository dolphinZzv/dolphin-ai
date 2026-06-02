package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config provides dot-notation access to configuration values.
type Config struct {
	values map[string]any
}

func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var raw map[string]any
	switch ext := filepath.Ext(path); ext {
	case ".json":
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	default:
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	for k, v := range flatten(raw, "") {
		cfg.values[k] = v
	}
	cfg.applyEnvOverrides()
	return cfg, nil
}

func LoadConfigFromMap(values map[string]any) *Config {
	return &Config{values: values}
}

// defaultConfig returns a Config populated with sensible defaults.
func defaultConfig() *Config {
	return &Config{
		values: map[string]any{
			"log.level":         "info",
			"log.max_size":      100,
			"log.max_backups":   30,
			"log.max_age":       30,
			"log.compress":      true,
			"tool.timeout":      "30s",
			"agent.name":        "Dolphin",
			"agent.workspace":   ".",
			"agent.workmode":    "default",
			"agent.max_rounds":  100,
			"agent.buffer_size": 1024,
			"permission.file":   "permissions.json",
			"memory.window":     40,
			"memory.dir":        ".dolphin/sessions",
			"brain.dir":         ".dolphin/brain",
		},
	}
}

// DetectLang attempts to detect the system language from environment variables.
// Returns "en" as fallback if detection fails.
func DetectLang() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	if lang == "" {
		lang = os.Getenv("LC_MESSAGES")
	}
	if lang == "" {
		return "en"
	}

	// Parse locale string: "zh_CN.UTF-8" → "zh", "en_US.UTF-8" → "en", "ja_JP" → "ja"
	if idx := strings.Index(lang, "_"); idx > 0 {
		return lang[:idx]
	}
	if idx := strings.Index(lang, "."); idx > 0 {
		return lang[:idx]
	}
	return lang
}

// Validate checks required configuration fields and returns an error if any are missing.
func (c *Config) Validate() error {
	// Check for new-style multi-provider config: any known provider with api_key.
	knownProviders := []string{"openai", "anthropic"}
	hasProvider := false
	for _, name := range knownProviders {
		if c.GetString("llm."+name+".api_key") != "" {
			hasProvider = true
			break
		}
	}
	if !hasProvider {
		// Legacy single-provider mode.
		provider := c.GetString("llm.provider")
		if provider == "" {
			return fmt.Errorf("config: missing llm.provider")
		}
		required := []string{"llm.provider", "llm.model", "llm." + provider + ".api_key"}
		var missing []string
		for _, key := range required {
			if c.GetString(key) == "" {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("config: missing required fields: %s", strings.Join(missing, ", "))
		}
	}
	if c.GetString("llm.model") == "" {
		return fmt.Errorf("config: missing llm.model")
	}
	return nil
}

// Get returns the raw value for a key, or nil if not set.
func (c *Config) Get(key string) any {
	if c.values == nil {
		return nil
	}
	return c.values[key]
}

func (c *Config) GetString(key string) string {
	v, ok := c.values[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// GetStringMap returns all values under the given dot-notation prefix.
// For prefix "a.b", keys "a.b.c" → {"c": value}. Only single-level keys.
func (c *Config) GetStringMap(prefix string) map[string]string {
	result := make(map[string]string)
	prefix = prefix + "."
	for _, key := range c.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(key, prefix)
		if strings.Contains(suffix, ".") {
			continue
		}
		if val := c.GetString(key); val != "" {
			result[suffix] = val
		}
	}
	return result
}

func (c *Config) GetInt(key string) int {
	v, ok := c.values[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	}
	return 0
}

func (c *Config) GetFloat(key string) float64 {
	v, ok := c.values[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

func (c *Config) GetBool(key string) bool {
	v, ok := c.values[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (c *Config) GetDuration(key string) time.Duration {
	v, ok := c.values[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case time.Duration:
		return val
	case string:
		d, _ := time.ParseDuration(val)
		return d
	case int:
		return time.Duration(val) * time.Second
	case float64:
		return time.Duration(val) * time.Second
	}
	return 0
}

func (c *Config) Keys() []string {
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	return keys
}

func (c *Config) Set(key string, value any) {
	if c.values == nil {
		c.values = make(map[string]any)
	}
	c.values[key] = value
}

// flatten converts a nested YAML map into dot-notation keys.
func flatten(data map[string]any, prefix string) map[string]any {
	result := make(map[string]any)
	for key, val := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		switch v := val.(type) {
		case map[string]any:
			for k, vv := range flatten(v, fullKey) {
				result[k] = vv
			}
		case []any:
			// Store arrays as indexed subkeys
			for i, item := range v {
				ik := fullKey + "." + strconv.Itoa(i)
				switch iv := item.(type) {
				case map[string]any:
					for k, vv := range flatten(iv, ik) {
						result[k] = vv
					}
				default:
					result[ik] = iv
				}
			}
		default:
			result[fullKey] = v
		}
	}
	return result
}

// applyEnvOverrides applies DOLPHIN_ prefixed env vars over config values.
// DOLPHIN_LLM_PROVIDER → key "llm.provider"
// Case-insensitive matching preserves the original casing of existing keys
// (e.g. DOLPHIN_OTEL_HEADERS_AUTHORIZATION matches otel.headers.Authorization).
func (c *Config) applyEnvOverrides() {
	if c.values == nil {
		return
	}

	// Build a case-insensitive lookup of existing keys.
	ci := make(map[string]string, len(c.values))
	for k := range c.values {
		ci[strings.ToLower(k)] = k
	}

	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "DOLPHIN_") {
			continue
		}
		eqIdx := strings.IndexByte(env, '=')
		if eqIdx < 0 {
			continue
		}
		envKey := env[:eqIdx]
		envVal := env[eqIdx+1:]

		dotKey := strings.ToLower(strings.TrimPrefix(envKey, "DOLPHIN_"))
		dotKey = strings.ReplaceAll(dotKey, "_", ".")

		// Preserve original casing if key already exists.
		if orig, ok := ci[dotKey]; ok {
			c.values[orig] = envVal
		} else {
			c.values[dotKey] = envVal
		}
	}
}
