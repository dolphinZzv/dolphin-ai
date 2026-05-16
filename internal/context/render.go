package context

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"dolphin/internal/config"

	"github.com/mitchellh/mapstructure"
	"go.uber.org/zap"
)

// RenderData holds the data context for template expansion in context files.
// Config is a nested map so template expressions like {{.Config.llm.model}}
// work naturally via Go's dot-chaining.
type RenderData struct {
	Config map[string]any
	Env    map[string]string
}

// NewRenderData builds a RenderData from the application config and environment.
func NewRenderData(cfg *config.Config) *RenderData {
	if cfg == nil {
		return nil
	}

	configMap := nestConfig(cfg)

	// Snapshot environment.
	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			envMap[k] = v
		}
	}

	return &RenderData{
		Config: configMap,
		Env:    envMap,
	}
}

// nestConfig converts a Config struct to a nested map[string]any, using
// the "mapstructure" struct tags as keys (same tags viper uses for YAML unmarshaling).
func nestConfig(cfg *config.Config) map[string]any {
	var result map[string]any
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:  &result,
		TagName: "mapstructure",
	})
	if err != nil {
		zap.S().Warnw("nest config: create decoder failed", "error", err)
		return nil
	}
	if err := dec.Decode(cfg); err != nil {
		zap.S().Warnw("nest config: decode failed", "error", err)
		return nil
	}
	return result
}

// funcMap returns the template function map available in context file templates.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"default": func(val any, fallback string) string {
			if val == nil {
				return fallback
			}
			s := fmt.Sprint(val)
			if s == "" || s == "<no value>" {
				return fallback
			}
			return s
		},
		"env": os.Getenv,
		"get": func(m map[string]any, path string) any {
			parts := strings.Split(path, ".")
			var cur any = m
			for _, p := range parts {
				cm, ok := cur.(map[string]any)
				if !ok {
					return nil
				}
				cur = cm[p]
			}
			return cur
		},
	}
}

// expandTemplate parses and executes a Go text/template with the given data.
// Returns the rendered string, or the original content on error.
func expandTemplate(name, content string, data *RenderData) string {
	if data == nil {
		return content
	}

	tmpl, err := template.New(name).Funcs(funcMap()).Parse(content)
	if err != nil {
		zap.S().Warnw("template parse failed, using raw content", "name", name, "error", err)
		return content
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		zap.S().Warnw("template execute failed, using raw content", "name", name, "error", err)
		return content
	}
	return buf.String()
}
