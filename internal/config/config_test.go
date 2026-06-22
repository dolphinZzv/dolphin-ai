package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	. "github.com/smartystreets/goconvey/convey"
	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	Convey("defaultConfig", t, func() {
		cfg := defaultConfig()
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("log.level"), ShouldEqual, "info")
		So(cfg.GetString("agent.name"), ShouldEqual, "Dolphin")
		So(cfg.GetInt("log.max_size"), ShouldEqual, 100)
	})
}

func TestLoadConfigFromMap(t *testing.T) {
	Convey("LoadConfigFromMap", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"llm.use":   "claude-3",
			"log.level": "debug",
		})
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("llm.use"), ShouldEqual, "claude-3")
		So(cfg.GetString("log.level"), ShouldEqual, "debug")
	})
}

func TestLoadConfig(t *testing.T) {
	Convey("LoadConfig from YAML file", t, func() {
		dir := t.TempDir()
		yamlContent := []byte(`
llm:
  use: gpt-4
log:
  level: debug
`)
		path := filepath.Join(dir, "config.yaml")
		_ = os.WriteFile(path, yamlContent, 0o644)

		cfg, err := LoadConfig(path)
		So(err, ShouldBeNil)
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("llm.use"), ShouldEqual, "gpt-4")
		So(cfg.GetString("log.level"), ShouldEqual, "debug")
	})

	Convey("LoadConfig returns error for missing file", t, func() {
		_, err := LoadConfig("/nonexistent/path.yaml")
		So(err, ShouldNotBeNil)
	})
}

func TestConfigGet(t *testing.T) {
	Convey("Config typed getters", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"name":         "test",
			"count":        42,
			"ratio":        3.14,
			"enabled":      true,
			"timeout":      "30s",
			"empty":        "",
			"zero":         0,
			"string_int":   "100",
			"string_float": "2.5",
		})

		Convey("Get returns raw value", func() {
			So(cfg.Get("name"), ShouldEqual, "test")
			So(cfg.Get("count"), ShouldEqual, 42)
			So(cfg.Get("nonexistent"), ShouldBeNil)
		})

		Convey("GetString", func() {
			So(cfg.GetString("name"), ShouldEqual, "test")
			So(cfg.GetString("empty"), ShouldEqual, "")
			So(cfg.GetString("nonexistent"), ShouldEqual, "")
		})

		Convey("GetInt", func() {
			So(cfg.GetInt("count"), ShouldEqual, 42)
			So(cfg.GetInt("zero"), ShouldEqual, 0)
			So(cfg.GetInt("string_int"), ShouldEqual, 100)
			So(cfg.GetInt("nonexistent"), ShouldEqual, 0)
		})

		Convey("GetFloat", func() {
			So(cfg.GetFloat("ratio"), ShouldEqual, 3.14)
			So(cfg.GetFloat("string_float"), ShouldEqual, 2.5)
			So(cfg.GetFloat("nonexistent"), ShouldEqual, 0)
		})

		Convey("GetBool", func() {
			So(cfg.GetBool("enabled"), ShouldBeTrue)
			So(cfg.GetBool("nonexistent"), ShouldBeFalse)
		})

		Convey("GetDuration", func() {
			So(cfg.GetDuration("timeout"), ShouldEqual, 30*time.Second)
			So(cfg.GetDuration("nonexistent"), ShouldEqual, 0)
		})
	})
}

func TestConfigKeys(t *testing.T) {
	Convey("Keys returns all keys", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"a.b": 1,
			"c.d": 2,
			"e.f": 3,
		})
		keys := cfg.Keys()
		So(len(keys), ShouldEqual, 3)
	})
}

func TestConfigSet(t *testing.T) {
	Convey("Set overrides a value", t, func() {
		cfg := LoadConfigFromMap(map[string]any{"key": "old"})
		So(cfg.GetString("key"), ShouldEqual, "old")
		cfg.Set("key", "new")
		So(cfg.GetString("key"), ShouldEqual, "new")
	})
}

func TestConfigValidate(t *testing.T) {
	Convey("Validate", t, func() {
		Convey("passes when llm.use is set", func() {
			cfg := LoadConfigFromMap(map[string]any{
				"llm.use": "gpt-4",
			})
			err := cfg.Validate()
			So(err, ShouldBeNil)
		})

		Convey("fails when llm.use is missing", func() {
			cfg := LoadConfigFromMap(map[string]any{})
			err := cfg.Validate()
			So(err, ShouldNotBeNil)
		})
	})
}

func TestFlatten(t *testing.T) {
	Convey("flatten converts nested map to dot notation", t, func() {
		input := map[string]any{
			"llm": map[string]any{
				"use": "claude-3",
			},
			"log": map[string]any{
				"level": "info",
			},
		}
		result := flatten(input, "")
		So(result["llm.use"], ShouldEqual, "claude-3")
		So(result["log.level"], ShouldEqual, "info")
	})
}

func TestApplyEnvOverrides(t *testing.T) {
	Convey("applyEnvOverrides applies DOLPHIN_ env vars", t, func() {
		_ = os.Setenv("DOLPHIN_LLM_USE", "gpt-4-turbo")
		defer func() { _ = os.Unsetenv("DOLPHIN_LLM_USE") }()

		cfg := LoadConfigFromMap(map[string]any{
			"llm.use": "gpt-4",
		})
		cfg.applyEnvOverrides()
		So(cfg.GetString("llm.use"), ShouldEqual, "gpt-4-turbo")
	})
}

func TestConfigNilSafety(t *testing.T) {
	Convey("Config methods handle nil values map", t, func() {
		cfg := &Config{}
		So(cfg.GetString("x"), ShouldEqual, "")
		So(cfg.GetInt("x"), ShouldEqual, 0)
		So(cfg.GetFloat("x"), ShouldEqual, 0)
		So(cfg.GetBool("x"), ShouldBeFalse)
		So(cfg.GetDuration("x"), ShouldEqual, 0)
		So(cfg.Keys(), ShouldBeEmpty)
		So(cfg.Get("x"), ShouldBeNil)
	})
}

func TestDetectLang(t *testing.T) {
	Convey("DetectLang", t, func() {
		Convey("returns en fallback when no env vars set", func() {
			_ = os.Unsetenv("LANG")
			_ = os.Unsetenv("LC_ALL")
			_ = os.Unsetenv("LC_MESSAGES")
			So(DetectLang(), ShouldEqual, "en")
		})

		Convey("parses zh from LANG env", func() {
			_ = os.Setenv("LANG", "zh_CN.UTF-8")
			defer func() { _ = os.Unsetenv("LANG") }()
			So(DetectLang(), ShouldEqual, "zh")
		})

		Convey("parses en from LANG env", func() {
			_ = os.Setenv("LANG", "en_US.UTF-8")
			defer func() { _ = os.Unsetenv("LANG") }()
			So(DetectLang(), ShouldEqual, "en")
		})

		Convey("falls back to LC_ALL", func() {
			_ = os.Unsetenv("LANG")
			_ = os.Setenv("LC_ALL", "ja_JP.UTF-8")
			defer func() { _ = os.Unsetenv("LC_ALL") }()
			So(DetectLang(), ShouldEqual, "ja")
		})

		Convey("falls back to LC_MESSAGES", func() {
			_ = os.Unsetenv("LANG")
			_ = os.Unsetenv("LC_ALL")
			_ = os.Setenv("LC_MESSAGES", "fr_FR.UTF-8")
			defer func() { _ = os.Unsetenv("LC_MESSAGES") }()
			So(DetectLang(), ShouldEqual, "fr")
		})

		Convey("handles lang without suffix", func() {
			_ = os.Setenv("LANG", "zh")
			defer func() { _ = os.Unsetenv("LANG") }()
			So(DetectLang(), ShouldEqual, "zh")
		})

		Convey("handles lang with only region", func() {
			_ = os.Setenv("LANG", "en_US")
			defer func() { _ = os.Unsetenv("LANG") }()
			So(DetectLang(), ShouldEqual, "en")
		})

		Convey("handles lang with dot separator", func() {
			_ = os.Setenv("LANG", "C.UTF-8")
			defer func() { _ = os.Unsetenv("LANG") }()
			So(DetectLang(), ShouldEqual, "C")
		})
	})
}

func TestGetStringMap(t *testing.T) {
	Convey("GetStringMap", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"llm.openai.api_key": "sk-abc",
			"llm.openai.model":   "gpt-4",
			"llm.anthropic.key":  "sk-xyz",
			"llm.use":            "openai",
			"agent.name":         "Dolphin",
		})

		Convey("returns single-level keys under prefix", func() {
			m := cfg.GetStringMap("llm.openai")
			So(m["api_key"], ShouldEqual, "sk-abc")
			So(m["model"], ShouldEqual, "gpt-4")
			So(len(m), ShouldEqual, 2)
		})

		Convey("returns empty map for prefix with no matches", func() {
			m := cfg.GetStringMap("nonexistent")
			So(m, ShouldBeEmpty)
			So(len(m), ShouldEqual, 0)
		})

		Convey("excludes multi-level keys beyond prefix", func() {
			m := cfg.GetStringMap("llm")
			So(m["use"], ShouldEqual, "openai")
			So(len(m), ShouldBeGreaterThan, 0)
		})

		Convey("handles empty config gracefully", func() {
			empty := &Config{}
			m := empty.GetStringMap("llm")
			So(m, ShouldBeEmpty)
			So(len(m), ShouldEqual, 0)
		})
	})
}

func TestConfigAgainstSchema(t *testing.T) {
	Convey("config.yaml validates against config.schema.json", t, func() {
		schemaPath := filepath.Join("..", "..", "config.schema.json")
		configPath := filepath.Join("..", "..", "config.yaml")

		schemaData, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Skipf("schema file not found: %v", err)
		}

		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("schema.json", strings.NewReader(string(schemaData))); err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile("schema.json")
		if err != nil {
			t.Fatalf("invalid schema: %v", err)
		}

		yamlData, err := os.ReadFile(configPath)
		if err != nil {
			t.Skipf("config file not found: %v", err)
		}

		var raw any
		if err := yaml.Unmarshal(yamlData, &raw); err != nil {
			t.Fatalf("invalid yaml: %v", err)
		}

		normalized := normalizeForJSON(raw)

		if err := schema.Validate(normalized); err != nil {
			t.Fatalf("config.yaml does not match schema: %v", err)
		}
	})
}

func TestParseHumanCount(t *testing.T) {
	Convey("parseHumanCount", t, func() {
		Convey("returns 0 for empty string", func() {
			So(parseHumanCount(""), ShouldEqual, 0)
		})

		Convey("parses plain integer", func() {
			So(parseHumanCount("42"), ShouldEqual, 42)
		})

		Convey("parses k/K suffix", func() {
			So(parseHumanCount("1k"), ShouldEqual, 1000)
			So(parseHumanCount("2K"), ShouldEqual, 2000)
		})

		Convey("parses m/M suffix", func() {
			So(parseHumanCount("1m"), ShouldEqual, 1000000)
			So(parseHumanCount("3M"), ShouldEqual, 3000000)
		})

		Convey("parses b/B suffix", func() {
			So(parseHumanCount("2b"), ShouldEqual, 2000000000)
			So(parseHumanCount("1B"), ShouldEqual, 1000000000)
		})

		Convey("parses t/T suffix", func() {
			So(parseHumanCount("1t"), ShouldEqual, 1000000000000)
			So(parseHumanCount("2T"), ShouldEqual, 2000000000000)
		})

		Convey("parses float with suffix", func() {
			So(parseHumanCount("1.5k"), ShouldEqual, 1500)
			So(parseHumanCount("2.5m"), ShouldEqual, 2500000)
		})

		Convey("returns 0 for unparseable input", func() {
			So(parseHumanCount("abc"), ShouldEqual, 0)
		})

		Convey("trims spaces", func() {
			So(parseHumanCount("  100  "), ShouldEqual, 100)
		})
	})
}

func TestGetIntWithHumanCount(t *testing.T) {
	Convey("GetInt with human-readable count", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"hcount": "1k",
			"hfloat": "1.5m",
		})
		So(cfg.GetInt("hcount"), ShouldEqual, 1000)
		So(cfg.GetInt("hfloat"), ShouldEqual, 1500000)
	})
}

func TestGetFloatWithString(t *testing.T) {
	Convey("GetFloat with string value", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"str_float": "3.14",
			"str_int":   "42",
		})
		So(cfg.GetFloat("str_float"), ShouldEqual, 3.14)
		So(cfg.GetFloat("str_int"), ShouldEqual, 42.0)
	})
}

func TestGetDurationExtended(t *testing.T) {
	Convey("GetDuration with various input types", t, func() {
		cfg := LoadConfigFromMap(map[string]any{
			"dur_str":   "30s",
			"dur_int":   60,         // interpreted as seconds
			"dur_float": 120.0,      // interpreted as seconds
			"dur_bad":   "not-a-duration",
		})
		So(cfg.GetDuration("dur_str"), ShouldEqual, 30*time.Second)
		So(cfg.GetDuration("dur_int"), ShouldEqual, 60*time.Second)
		So(cfg.GetDuration("dur_float"), ShouldEqual, 120*time.Second)
		So(cfg.GetDuration("dur_bad"), ShouldEqual, 0)
	})
}

func TestSetOnNilValues(t *testing.T) {
	Convey("Set on nil values map creates it", t, func() {
		cfg := &Config{}
		cfg.Set("new.key", "value")
		So(cfg.GetString("new.key"), ShouldEqual, "value")
	})
}

func TestFlattenWithArrays(t *testing.T) {
	Convey("flatten with nested arrays", t, func() {
		input := map[string]any{
			"llm": map[string]any{
				"models": []any{
					map[string]any{"name": "gpt-4"},
					map[string]any{"name": "gpt-3.5"},
				},
			},
			"tags": []any{"a", "b", "c"},
		}
		result := flatten(input, "")
		So(result["llm.models.0.name"], ShouldEqual, "gpt-4")
		So(result["llm.models.1.name"], ShouldEqual, "gpt-3.5")
		So(result["tags.0"], ShouldEqual, "a")
		So(result["tags.1"], ShouldEqual, "b")
		So(result["tags.2"], ShouldEqual, "c")
	})
}

func TestLoadConfigJSON(t *testing.T) {
	Convey("LoadConfig from JSON file", t, func() {
		dir := t.TempDir()
		jsonContent := []byte(`{"llm":{"use":"claude-3"},"log":{"level":"warn"}}`)
		path := filepath.Join(dir, "config.json")
		_ = os.WriteFile(path, jsonContent, 0o644)

		cfg, err := LoadConfig(path)
		So(err, ShouldBeNil)
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("llm.use"), ShouldEqual, "claude-3")
		So(cfg.GetString("log.level"), ShouldEqual, "warn")
	})
}

func TestApplyEnvOverridesNewKey(t *testing.T) {
	Convey("applyEnvOverrides with new env var", t, func() {
		_ = os.Setenv("DOLPHIN_CUSTOM_KEY", "custom_value")
		defer func() { _ = os.Unsetenv("DOLPHIN_CUSTOM_KEY") }()

		cfg := LoadConfigFromMap(map[string]any{})
		cfg.applyEnvOverrides()
		So(cfg.GetString("custom.key"), ShouldEqual, "custom_value")
	})
}

func normalizeForJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = normalizeForJSON(vv)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k.(string)] = normalizeForJSON(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = normalizeForJSON(vv)
		}
		return out
	default:
		return v
	}
}
