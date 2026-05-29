package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
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
			"llm.provider": "anthropic",
			"llm.model":    "claude-3",
			"log.level":    "debug",
		})
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("llm.provider"), ShouldEqual, "anthropic")
		So(cfg.GetString("llm.model"), ShouldEqual, "claude-3")
		So(cfg.GetString("log.level"), ShouldEqual, "debug")
	})
}

func TestLoadConfig(t *testing.T) {
	Convey("LoadConfig from YAML file", t, func() {
		dir := t.TempDir()
		yamlContent := []byte(`
llm:
  provider: openai
  model: gpt-4
log:
  level: debug
`)
		path := filepath.Join(dir, "config.yaml")
		os.WriteFile(path, yamlContent, 0644)

		cfg, err := LoadConfig(path)
		So(err, ShouldBeNil)
		So(cfg, ShouldNotBeNil)
		So(cfg.GetString("llm.provider"), ShouldEqual, "openai")
		So(cfg.GetString("llm.model"), ShouldEqual, "gpt-4")
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
		Convey("passes when required fields are present", func() {
			cfg := LoadConfigFromMap(map[string]any{
				"llm.provider":       "openai",
				"llm.model":          "gpt-4",
				"llm.openai.api_key": "sk-test",
			})
			err := cfg.Validate()
			So(err, ShouldBeNil)
		})

		Convey("fails when llm.provider is missing", func() {
			cfg := LoadConfigFromMap(map[string]any{})
			err := cfg.Validate()
			So(err, ShouldNotBeNil)
		})

		Convey("fails when api_key is missing", func() {
			cfg := LoadConfigFromMap(map[string]any{
				"llm.provider": "openai",
				"llm.model":    "gpt-4",
			})
			err := cfg.Validate()
			So(err, ShouldNotBeNil)
		})
	})
}

func TestFlatten(t *testing.T) {
	Convey("flatten converts nested map to dot notation", t, func() {
		input := map[string]any{
			"llm": map[string]any{
				"provider": "anthropic",
				"model":    "claude-3",
			},
			"log": map[string]any{
				"level": "info",
			},
		}
		result := flatten(input, "")
		So(result["llm.provider"], ShouldEqual, "anthropic")
		So(result["llm.model"], ShouldEqual, "claude-3")
		So(result["log.level"], ShouldEqual, "info")
	})
}

func TestApplyEnvOverrides(t *testing.T) {
	Convey("applyEnvOverrides applies DOLPHIN_ env vars", t, func() {
		os.Setenv("DOLPHIN_LLM_MODEL", "gpt-4-turbo")
		defer os.Unsetenv("DOLPHIN_LLM_MODEL")

		cfg := LoadConfigFromMap(map[string]any{
			"llm.model": "gpt-4",
		})
		cfg.applyEnvOverrides()
		So(cfg.GetString("llm.model"), ShouldEqual, "gpt-4-turbo")
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
