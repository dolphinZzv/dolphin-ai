package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/i18n"

	"gopkg.in/yaml.v3"
)

// configTemplateEN is the default config with English comments.
const configTemplateEN = `# dolphin configuration
# This file is auto-generated. Edit and restart to apply changes.

# ── LLM Provider ──────────────────────────────────────────
# Primary provider + automatic failover. Startup tries each in order
# and uses the first provider that responds.
llm:
  providers:
    - name: claude
      type: anthropic
      api_key: ""             # set via DZ_LLM_API_KEY env var instead
      base_url: https://api.anthropic.com/v1
      model: claude-sonnet-4-6
    - name: deepseek
      type: openai
      api_key: ""
      base_url: https://api.deepseek.com
      model: deepseek-v4-flash

  # Legacy single-provider fields (fallback when providers: is not set).
  type: anthropic
  base_url: https://api.anthropic.com/v1
  model: claude-sonnet-4-6
  api_key: ""             # set via DZ_LLM_API_KEY env var instead
  max_tokens: 8192
  max_context_tokens: 1048576  # trigger context compression above 70% of this
  temperature: 0.7        # 0.0–2.0
  max_sub_turns: 10       # max tool-call feedback loops per user turn

  # ── LLM Usage Limits ─────────────────────────────────────
  limits:
    enabled: false
    scheduler_enabled: true
    requests:
      daily_max: 1000
      daily_reset_cron: "0 0 * * *"
      weekly_max: 5000
      weekly_reset_cron: "0 0 * * 1"
      monthly_max: 10000
      monthly_reset_cron: "0 0 1 * *"
    tokens:
      daily_input_max: 1000000
      daily_output_max: 500000
      daily_reset_cron: "0 0 * * *"
      weekly_input_max: 5000000
      weekly_output_max: 2000000
      weekly_reset_cron: "0 0 * * 1"
      monthly_input_max: 10000000
      monthly_output_max: 5000000
      monthly_reset_cron: "0 0 1 * *"
    concurrency:
      max_running: 5
    enforcement: hard
    retry:
      max_attempts: 3
      initial_backoff: 1s
      max_backoff: 60s
    exempt:
      enabled: false
      patterns: []

# ── Sessions ──────────────────────────────────────────────
session:
  dir: .dolphin/sessions
  max_loop: 50            # max turns per session before checkpoint
  summary: true           # auto-generate session summary
  max_age: 24h            # auto-delete sessions older than this
  resume: false           # prompt to resume last session on start

# ── MCP Tools ─────────────────────────────────────────────
mcp:
  shell:
    enabled: true
    timeout_seconds: 30
    priority: 10
    max_command_length: 4096
    # allowed_commands: [ls, cat, grep, find, wc, head, tail]  # empty = allow all
  cdp:
    enabled: true         # browser automation (Chrome DevTools Protocol)
    headless: true
    priority: 1000
    idle_timeout: 300     # seconds before auto-closing idle browser
    startup_timeout: 30   # seconds before giving up on browser init verify
  email:
    enabled: true         # email send/search/fetch MCP tool (uses transport.email config)
    priority: 500
  servers: {}             # external MCP servers, e.g. myserver: {type: stdio, command: npx, args: [...]}
  #                       #   or remote: {type: sse, url: "https://...", headers: {Authorization: "Bearer ..."}}
  #                       #   or remote: {type: http-stream, url: "https://..."}
  repos: []               # manifest repos, e.g. ["dolphinv/mcp"]
  webhook:
    enabled: true
    priority: 100
    # targets:              # named webhook targets, e.g.
    #   my_bot:             #   name referenced by the webhook tool
    #     url: "https://hooks.example.com/webhook"
    #     method: POST
    #     headers: {Authorization: "Bearer my-token"}

# ── Agent Pool ────────────────────────────────────────────
agent_pool:
  max_concurrency: 5      # max simultaneous sub-agent tasks
  default_timeout: 300    # seconds per task
  workspace_dir: .dolphin/workspaces
  idle_timeout: 600       # seconds before reaping idle temp agents
  max_pending_results: 10
  max_pending_result_len: 500  # chars per result in prompt, 0 = no truncation

# ── Skills ────────────────────────────────────────────────
skills:
  dir: .dolphin/skills
  max_top: 10             # skills shown in LLM context
  repos: []               # manifest repos, e.g. ["dolphinv/skills"]

# ── Transports ────────────────────────────────────────────
transport:
  stdio:
    enabled: true
  ssh:
    enabled: false
    addr: ":2222"
    host_key: "~/.ssh/id_ed25519"
    username: dolphinnzZ
    password: ""          # auto-generated on first SSH start
  mqtt:
    enabled: false
    broker: tcp://localhost:1883
    subscribe_topic: /agent/dolphin
    publish_topic: /agent/dolphin/message
    client_id: dolphinnzZ-agent
    username: ""              # MQTT client credentials for broker connection
    password: ""
  email:
    enabled: false
    smtp_host: ""
    smtp_port: 587
    imap_host: ""
    imap_port: 993
    username: ""
    password: ""          # set via DZ_EMAIL_PASSWORD env var instead
    from: ""
    use_tls: true
    poll_interval: 10s

# ── Servers ───────────────────────────────────────────────
servers:
  mqtt_broker:
    enabled: true
    addr: :1883
    accounts:
      - username: dolphinnzZ
        password: ""          # auto-generated if empty; set manually to override

# ── Crontab ───────────────────────────────────────────────
crontab:
  file: .dolphin/CRONTAB.md
  check_interval: 30s

# ── Diary (session summary aggregation) ───────────────────
diary:
  dir: .dolphin/diary
  max_day_sessions: 200       # sessions per day before pruning oldest
  max_week_days: 7            # days per week before pruning oldest
  max_month_weeks: 5          # weeks per month before pruning oldest
  max_year_months: 12         # months per year before pruning oldest
  max_total_mb: 500           # total diary size limit, deletes oldest year

# ── Observability ─────────────────────────────────────────
log_level: info
log_file: .dolphin/logs/agent.log

# ── Silent Update ──────────────────────────────────────────
update:
  enabled: true             # periodically check for new versions
  check_interval: 24h       # how often to check (e.g. "24h", "12h", "1h")
  channel: stable           # "stable" (releases only) or "pre-release"
  auto_install: false       # automatically install updates (false = notify only in logs)

# Hook (sync): intercept agent loop at session:start, user:input, llm:*, tool:*
# Event (async): subscribe to notifications via webhook or JSONL log
# Place script plugins in ~/.dolphin/plugins/ to register handlers
plugins:
  enabled: true
  dir: "~/.dolphin/plugins/"
  webhook_url: ""                 # POST events as JSON to HTTP endpoint
  webhook_events: ["*"]           # event types: session:*, user:*, llm:*, tool:*, agent:*, error
  heartbeat_turns: 0              # emit heartbeat every N turns (0 = off)

pprof:
  enabled: false
  addr: ":6060"

metrics:
  enabled: false
  addr: ":9090"

telemetry:
  enabled: false
  service_name: dolphin
  exporter: stdout                    # stdout, otlp-grpc, otlp-http
  otlp_endpoint: localhost:4317
  sample_rate: 1.0
  # otlp_headers:                     # custom headers for OTLP exporters
  #   Authorization: Bearer <token>
  #   stream-name: default
`

// restrictiveNotes is appended to the standard template to document security
// hardening applied in restrictive mode. This approach reuses the standard
// template comments but overrides specific values via yaml overlay below.
const restrictiveTemplateEN = `# ===== RESTRICTIVE MODE =====
# This config was generated with 'dolphin init --restrictive'.
# Security-hardened settings:
#   - Shell: only allowlisted commands (ls, cat, grep, find, ...)
#   - CDP browser automation: disabled
#   - Webhook tool: disabled
#   - Log level: warn (reduces secret leakage in logs)
#   - Plugins: disabled
# =============================

`
const restrictiveTemplateZH = `# ===== 限制模式 =====
# 此配置由 'dolphin init --restrictive' 生成。
# 安全加固设置：
#   - Shell: 仅允许白名单命令（ls、cat、grep、find 等）
#   - CDP 浏览器自动化：禁用
#   - Webhook 工具：禁用
#   - 日志级别：warn（减少日志中的密钥泄露风险）
#   - 插件：禁用
# =======================

`

// configTemplateZH is the default config with Chinese comments.
const configTemplateZH = `# dolphin 配置文件
# 此文件由程序自动生成。修改后重启即可生效。

# ── LLM 提供商 ────────────────────────────────────────────
# 主 provider + 自动故障转移。启动时逐个检测，使用第一个可用的。
llm:
  providers:
    - name: claude
      type: anthropic
      api_key: ""             # 建议通过环境变量 DZ_LLM_API_KEY 设置
      base_url: https://api.anthropic.com/v1
      model: claude-sonnet-4-6
    - name: deepseek
      type: openai
      api_key: ""
      base_url: https://api.deepseek.com
      model: deepseek-v4-flash

  # 单 provider 遗留字段（未设置 providers: 时使用）。
  type: anthropic
  base_url: https://api.anthropic.com/v1
  model: claude-sonnet-4-6
  api_key: ""             # 建议通过环境变量 DZ_LLM_API_KEY 设置
  max_tokens: 8192
  max_context_tokens: 1048576  # 超过 70% 时触发上下文压缩
  temperature: 0.7        # 0.0–2.0
  max_sub_turns: 10       # 每轮用户输入最多工具调用反馈循环次数

# ── 会话 ──────────────────────────────────────────────────
session:
  dir: .dolphin/sessions
  max_loop: 50            # 每次会话最大轮数，超出后生成检查点
  summary: true           # 自动生成会话摘要
  max_age: 24h            # 超过此时间的会话文件自动清理
  resume: false           # 启动时是否提示恢复上次会话

# ── MCP 工具 ──────────────────────────────────────────────
mcp:
  shell:
    enabled: true
    timeout_seconds: 30
    priority: 10
    max_command_length: 4096
    # allowed_commands: [ls, cat, grep, find, wc, head, tail]  # empty = allow all
  cdp:
    enabled: true         # 浏览器自动化（Chrome DevTools Protocol）
    headless: true
    priority: 1000
    idle_timeout: 300     # 空闲多少秒后自动关闭浏览器
    startup_timeout: 30   # 浏览器启动验证超时时间（秒）
  servers: {}             # 外部 MCP 服务器，如 myserver: {type: stdio, command: npx, args: [...]}
  #                       #   或远程: {type: sse, url: "https://...", headers: {Authorization: "Bearer ..."}}
  #                       #   或远程: {type: http-stream, url: "https://..."}
  repos: []               # 清单仓库，如 ["dolphinv/mcp"]

# ── Agent 池 ──────────────────────────────────────────────
agent_pool:
  max_concurrency: 5      # 最大并发子 agent 任务数
  default_timeout: 300    # 每个任务的超时秒数
  workspace_dir: .dolphin/workspaces
  idle_timeout: 600       # 空闲临时 agent 多久后被回收（秒）
  max_pending_results: 10
  max_pending_result_len: 500  # prompt 中每条结果最大字符数，0 = 不截断

# ── 技能 ──────────────────────────────────────────────────
skills:
  dir: .dolphin/skills
  max_top: 10             # LLM 上下文中展示的技能数
  repos: []               # 清单仓库，如 ["dolphinv/skills"]

# ── 传输层 ────────────────────────────────────────────────
transport:
  stdio:
    enabled: true
  ssh:
    enabled: false
    addr: ":2222"
    host_key: "~/.ssh/id_ed25519"
    username: dolphinnzZ
    password: ""          # 首次 SSH 启动时自动生成
  mqtt:
    enabled: false
    broker: tcp://localhost:1883
    subscribe_topic: /agent/dolphin
    publish_topic: /agent/dolphin/message
    client_id: dolphinnzZ-agent
    username: ""              # MQTT 客户端连接 broker 的凭据
    password: ""
  email:
    enabled: false
    smtp_host: ""
    smtp_port: 587
    imap_host: ""
    imap_port: 993
    username: ""
    password: ""          # 建议通过环境变量 DZ_EMAIL_PASSWORD 设置
    from: ""
    use_tls: true
    poll_interval: 10s

# ── 服务器 ───────────────────────────────────────────────
servers:
  mqtt_broker:
    enabled: true
    addr: :1883
    accounts:
      - username: dolphinnzZ
        password: ""          # 为空则自动生成随机密码；手动设置则覆盖

# ── 定时任务 ──────────────────────────────────────────────
crontab:
  file: .dolphin/CRONTAB.md
  check_interval: 30s

# ── 日记（会话摘要聚合）───────────────────────────────────
diary:
  dir: .dolphin/diary
  max_day_sessions: 200       # 每天最多保留的会话数
  max_week_days: 7            # 每周最多保留的天数
  max_month_weeks: 5          # 每月最多保留的周数
  max_year_months: 12         # 每年最多保留的月数
  max_total_mb: 500           # 日记总大小上限，超出则删除最旧年份

# ── 可观测性 ──────────────────────────────────────────────
log_level: info
log_file: .dolphin/logs/agent.log

# ── 静默更新 ──────────────────────────────────────────────
update:
  enabled: true             # 定期检查新版本
  check_interval: 24h       # 检查间隔（如 "24h"、"12h"、"1h"）
  channel: stable           # "stable"（仅正式版）或 "pre-release"（含预发布版）
  auto_install: false       # 自动安装更新（false = 仅日志通知）

# Hook (sync): intercept agent loop at session:start, user:input, llm:*, tool:*
# Event (async): subscribe to notifications via webhook or JSONL log
# Place script plugins in ~/.dolphin/plugins/ to register handlers
plugins:
  enabled: true
  dir: "~/.dolphin/plugins/"
  webhook_url: ""                 # POST events as JSON to HTTP endpoint
  webhook_events: ["*"]           # event types: session:*, user:*, llm:*, tool:*, agent:*, error
  heartbeat_turns: 0              # emit heartbeat every N turns (0 = off)

pprof:
  enabled: false
  addr: ":6060"

metrics:
  enabled: false
  addr: ":9090"

telemetry:
  enabled: false
  service_name: dolphin
  exporter: stdout                    # stdout, otlp-grpc, otlp-http
  otlp_endpoint: localhost:4317
  sample_rate: 1.0
  # otlp_headers:                     # custom headers for OTLP exporters
  #   Authorization: Bearer <token>
  #   stream-name: default
`

func GenerateRestrictiveConfigFile(lang i18n.Lang) (string, error) {
	tmplEN := restrictiveTemplateEN + configTemplateEN
	tmplZH := restrictiveTemplateZH + configTemplateZH
	tmpl := tmplEN
	if lang == i18n.ZH {
		tmpl = tmplZH
	}

	path := filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	// Override security-hardened values on top of the base template
	hardened := map[string]any{
		"mcp": map[string]any{
			"shell": map[string]any{
				"allowed_commands": []string{"ls", "cat", "grep", "find", "pwd", "date", "echo", "head", "tail", "wc", "sort", "whoami", "uname"},
			},
			"cdp": map[string]any{
				"enabled": false,
			},
			"webhook": map[string]any{
				"enabled": false,
			},
		},
		"log_level": "warn",
		"plugins": map[string]any{
			"enabled": false,
		},
	}

	// Parse template as base YAML and merge hardened values
	base := make(map[string]any)
	if err := yaml.Unmarshal([]byte(tmpl), &base); err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	deepMerge(base, hardened)

	data, err := yaml.Marshal(base)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}

	// Prepend the restrictive header (YAML strips comments during marshal)
	header := restrictiveTemplateEN
	if lang == i18n.ZH {
		header = restrictiveTemplateZH
	}
	output := append([]byte(header), data...)

	if err := os.WriteFile(path, output, 0600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// GenerateConfigFile writes a commented default config to .dolphin/config.yaml.
// Comments adapt to the given language. Returns the file path written.
func GenerateConfigFile(lang i18n.Lang) (string, error) {
	tmpl := configTemplateEN
	if lang == i18n.ZH {
		tmpl = configTemplateZH
	}

	path := filepath.Join(ProjectConfigDir, ConfigFileName+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	if err := os.WriteFile(path, []byte(tmpl), 0600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		dv, exists := dst[k]
		if !exists {
			dst[k] = sv
			continue
		}
		srcMap, srcIsMap := sv.(map[string]any)
		dstMap, dstIsMap := dv.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[k] = sv
		}
	}
}

func PromptConfigFile() (bool, error) {
	lang := i18n.DetectLang()
	fmt.Fprintf(os.Stderr, "\n%s\n", i18n.T(i18n.KeyConfigPrompt, lang))
	fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeyConfigExplain, lang))
	fmt.Fprintf(os.Stderr, "  [y] %s  [r] %s  [n] %s\n",
		i18n.T(i18n.KeyConfigYes, lang), i18n.T(i18n.KeyConfigRestrictive, lang), i18n.T(i18n.KeyConfigNo, lang))
	fmt.Fprintf(os.Stderr, "%s: ", i18n.T(i18n.KeyChoice, lang))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, nil //nolint:nilerr
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "r" || input == "restrictive" {
		path, err := GenerateRestrictiveConfigFile(lang) //nolint:govet
		if err != nil {
			return false, fmt.Errorf("generate restrictive config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.T(i18n.KeyConfigGenerated, lang), path)
		fmt.Fprintf(os.Stderr, "  %s\n", i18n.T(i18n.KeyRestrictiveHint, lang))
		return true, nil
	}

	if input != "y" && input != "yes" {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeyConfigSkipped, lang))
		return false, nil
	}

	path, err := GenerateConfigFile(lang)
	if err != nil {
		return false, fmt.Errorf("generate config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", i18n.T(i18n.KeyConfigGenerated, lang), path)
	return true, nil
}
