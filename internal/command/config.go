package command

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed config.schema.json
var defaultConfigSchema []byte

const defaultConfigYAML = `# yaml-language-server: $schema=config.schema.json

# --- Language (en, zh) ---
lang: en

# --- LLM Configuration ---
llm:
  # Active model name. Must reference a model from one of the provider sections below.
  use: ""
  max_tokens: 4096
  max_retries: 3
  timeout: 120s
  # Global usage limits (optional)
  # limit:
  #   enabled: false
  #   max_requests: 1000
  #   max_total_tokens: 10000000
  #   max_input_tokens: 8000000
  #   max_output_tokens: 2000000
  #   reset_cron: "0 0 * * *"

# --- Logging ---
log:
  level: info
  max_size: 100
  max_backups: 30
  max_age: 30
  compress: true
  rotate_interval: "0"

# --- Tool Settings ---
tool:
  timeout: 30s

# --- Memory ---
memory:
  dir: .dolphin/memory

# --- Session ---
session:
  dir: .dolphin/sessions
  window: 40
  mode: per_transport

# --- Agent ---
agent:
  name: Dolphin
  max_rounds: 100
  pool_size: 1
  tool_parallelism: 1
  buffer_size: 1024
  turn_timeout: 120s
  session_gc_interval: 300s
  workspace: "."
  workmode: default

# --- Brain (skills, commands, scripts) ---
brain:
  dir: .dolphin/brain

# --- LLM Provider Examples ---
# Add your provider configurations here. Each section name (e.g. deepseek_openai)
# becomes a provider you can reference from llm.use.
#
# deepseek_openai:
#   provider: deepseek
#   api_type: openai
#   api_key: "sk-..."
#   base_url: https://api.deepseek.com
#   models:
#     - name: deepseek-chat
#     - name: deepseek-reasoner
#
# deepseek_anthropic:
#   provider: deepseek
#   api_type: anthropic
#   api_key: "sk-..."
#   base_url: https://api.deepseek.com/anthropic
#
# openai:
#   provider: openai
#   api_type: openai
#   api_key: "sk-..."
#   base_url: https://api.openai.com/v1
#   models:
#     - name: gpt-4o
#
# anthropic:
#   provider: anthropic
#   api_type: anthropic
#   api_key: "sk-ant-..."
#   models:
#     - name: claude-sonnet-4-6

# --- Transport: Stdio ---
stdio:
  enabled: true

# --- Transport: TUI (optional) ---
# tui:
#   enabled: false
#   show_thinking: false
#   show_tools: false

# --- Transport: A2A (optional) ---
# a2a:
#   enabled: false
#   addr: ":8100"

# --- Transport: DingTalk (optional) ---
# dingtalk:
#   enabled: false

# --- Transport: WeWork (optional) ---
# wework:
#   enabled: false

# --- Transport: Email (optional) ---
# email:
#   enabled: false

# --- Transport: Panda (optional) ---
# panda:
#   enabled: false

# --- OpenTelemetry (optional) ---
# otel:
#   enabled: false

# --- Prometheus Metrics (optional) ---
# prometheus:
#   enabled: false
#   mode: pull
#   addr: ":9090"

# --- pprof Profiling (optional) ---
# pprof:
#   enabled: false

# --- MCP Servers ---
# mcp_servers:
#   - name: shell
#     type: builtin
#     enabled: true
`

// NewConfigInitCmd creates the "init" subcommand for config.
func NewConfigInitCmd() *cobra.Command {
	initCmd := WithI18nShort(&cobra.Command{
		Use: "init",
		RunE: func(cmd *cobra.Command, args []string) error {
			outputPath, _ := cmd.Flags().GetString("output")
			force, _ := cmd.Flags().GetBool("force")
			skipSchema, _ := cmd.Flags().GetBool("skip-schema")

			if !force {
				if _, err := os.Stat(outputPath); err == nil {
					cmd.Printf("config.yaml already exists at %s. Use --force to overwrite.\n", outputPath)
					return nil
				}
			}

			if err := os.WriteFile(outputPath, []byte(defaultConfigYAML), 0644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			cmd.Printf("created default config at %s\n", outputPath)

			if !skipSchema {
				schemaPath := filepath.Join(filepath.Dir(outputPath), "config.schema.json")
				if !force {
					if _, err := os.Stat(schemaPath); err == nil {
						cmd.Printf("config.schema.json already exists at %s. Use --force to overwrite.\n", schemaPath)
						return nil
					}
				}
				if err := os.WriteFile(schemaPath, defaultConfigSchema, 0644); err != nil {
					return fmt.Errorf("write schema: %w", err)
				}
				cmd.Printf("created default schema at %s\n", schemaPath)
			}

			return nil
		},
	}, "command.config_init_desc")

	initCmd.Flags().StringP("output", "o", "config.yaml", "output path for the generated config file")
	initCmd.Flags().BoolP("force", "f", false, "overwrite existing config file")
	initCmd.Flags().Bool("skip-schema", false, "do not generate config.schema.json alongside config.yaml")
	return initCmd
}

// RegisterConfig registers the /config command group.
func RegisterConfig(r *Registry) {
	configCmd := WithI18nShort(&cobra.Command{
		Use: "config",
	}, "command.config_desc")

	configCmd.AddCommand(NewConfigInitCmd())
	r.Register(configCmd)
}
