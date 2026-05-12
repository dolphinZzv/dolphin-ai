package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dolphin/internal/i18n"
)

// configTemplateEN is the default config with English comments.
const configTemplateEN = `# dolphin configuration
# This file is auto-generated. Edit and restart to apply changes.

# ── LLM Provider ──────────────────────────────────────────
llm:
  type: openai            # "openai" or "anthropic"
  base_url: https://api.openai.com/v1
  model: gpt-4o
  api_key: ""             # set via DZ_LLM_API_KEY env var instead
  max_tokens: 4096
  max_context_tokens: 1048576  # trigger context compression above 70% of this
  temperature: 0.7        # 0.0–2.0
  max_sub_turns: 10       # max tool-call feedback loops per user turn

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
  cdp:
    enabled: true         # browser automation (Chrome DevTools Protocol)
    headless: true
    priority: 1000
    idle_timeout: 300     # seconds before auto-closing idle browser
  servers: {}             # external MCP servers, e.g. myserver: {type: stdio, command: npx, args: [...]}
  #                       #   or remote: {type: sse, url: "https://...", headers: {Authorization: "Bearer ..."}}
  #                       #   or remote: {type: http-stream, url: "https://..."}
  repos: []               # manifest repos, e.g. ["dolphinv/mcp"]

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
    topic: dolphinnzZ/agent/command
    response_topic: dolphinnzZ/agent/response
    client_id: dolphinnzZ-agent
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

pprof:
  enabled: false
  addr: ":6060"

metrics:
  enabled: false
  addr: ":9090"
`

// configTemplateZH is the default config with Chinese comments.
const configTemplateZH = `# dolphin 配置文件
# 此文件由程序自动生成。修改后重启即可生效。

# ── LLM 提供商 ────────────────────────────────────────────
llm:
  type: openai            # "openai" 或 "anthropic"
  base_url: https://api.openai.com/v1
  model: gpt-4o
  api_key: ""             # 建议通过环境变量 DZ_LLM_API_KEY 设置
  max_tokens: 4096
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
  cdp:
    enabled: true         # 浏览器自动化（Chrome DevTools Protocol）
    headless: true
    priority: 1000
    idle_timeout: 300     # 空闲多少秒后自动关闭浏览器
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
    topic: dolphinnzZ/agent/command
    response_topic: dolphinnzZ/agent/response
    client_id: dolphinnzZ-agent
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

pprof:
  enabled: false
  addr: ":6060"

metrics:
  enabled: false
  addr: ":9090"
`

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

	if err := os.WriteFile(path, []byte(tmpl), 0644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// PromptConfigFile asks the user whether to generate a commented default config.
// Returns true if the file was generated.
// PromptConfigFile asks the user whether to generate a commented default config.
// Returns true if the file was generated.
func PromptConfigFile() (bool, error) {
	lang := i18n.DetectLang()
	fmt.Fprintf(os.Stderr, "\n%s\n", i18n.T(i18n.KeyConfigPrompt, lang))
	fmt.Fprintf(os.Stderr, "%s\n", i18n.T(i18n.KeyConfigExplain, lang))
	fmt.Fprintf(os.Stderr, "  [y] %s  [n] %s\n",
		i18n.T(i18n.KeyConfigYes, lang), i18n.T(i18n.KeyConfigNo, lang))
	fmt.Fprintf(os.Stderr, "%s: ", i18n.T(i18n.KeyChoice, lang))

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, nil
	}
	input = strings.TrimSpace(strings.ToLower(input))

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
