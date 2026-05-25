package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	v.SetDefault("name", "dolphin")

	v.SetDefault("workspace", ".")

	v.SetDefault("llm.type", "openai")
	v.SetDefault("llm.base_url", "https://api.openai.com/v1")
	v.SetDefault("llm.model", "gpt-4o")
	v.SetDefault("llm.max_tokens", 4096)
	v.SetDefault("llm.temperature", 0.7)
	v.SetDefault("llm.max_sub_turns", 10)
	v.SetDefault("llm.max_context_tokens", 1048576)
	v.SetDefault("llm.compress_mode", "drop")
	v.SetDefault("llm.segment_merge_limit", 100)
	v.SetDefault("llm.timeout_seconds", 300)
	v.SetDefault("llm.health_check_timeout_seconds", 10)
	v.SetDefault("llm.compress_timeout_seconds", 15)
	v.SetDefault("llm.retry.max_attempts", 3)
	v.SetDefault("llm.retry.backoff_base", "1s")

	v.SetDefault("session.max_loop", 50)
	v.SetDefault("session.summary", true)
	v.SetDefault("session.max_size_mb", 10)

	v.SetDefault("transport.stdio.enabled", true)
	v.SetDefault("transport.stdio.markdown_render", true)
	v.SetDefault("transport.stdio.markdown_style", "auto")
	v.SetDefault("transport.ssh.enabled", false)
	v.SetDefault("transport.ssh.markdown_render", false)
	v.SetDefault("transport.ssh.read_timeout", "5m")
	v.SetDefault("transport.ssh.markdown_style", "auto")
	v.SetDefault("transport.ssh.addr", ":2222")
	v.SetDefault("transport.ssh.host_key", "~/.ssh/id_ed25519")
	v.SetDefault("transport.ssh.username", "dolphin")
	v.SetDefault("transport.ssh.password", "")
	v.SetDefault("transport.mqtt.enabled", false)
	v.SetDefault("transport.mqtt.broker", "tcp://localhost:1883")
	v.SetDefault("transport.mqtt.subscribe_topic", "/agent/dolphin")
	v.SetDefault("transport.mqtt.publish_topic", "/agent/dolphin/message")
	v.SetDefault("transport.mqtt.client_id", "dolphin-agent")
	v.SetDefault("transport.mqtt.keep_alive_seconds", 60)
	v.SetDefault("transport.mqtt.ping_timeout_seconds", 10)
	v.SetDefault("transport.mqtt.max_reconnect_seconds", 30)

	v.SetDefault("servers.mqtt_broker.enabled", true)
	v.SetDefault("servers.mqtt_broker.addr", ":1883")

	v.SetDefault("transport.email.enabled", false)
	v.SetDefault("transport.email.smtp_port", 587)
	v.SetDefault("transport.email.imap_port", 993)
	v.SetDefault("transport.email.dial_timeout", "30s")
	v.SetDefault("transport.email.use_tls", true)
	v.SetDefault("transport.email.poll_interval", "10s")
	v.SetDefault("transport.email.allowed_senders", []string{})

	v.SetDefault("transport.dingtalk.enabled", false)
	v.SetDefault("transport.dingtalk.read_timeout", "5m")

	v.SetDefault("transport.a2a.enabled", false)
	v.SetDefault("transport.a2a.listen_addr", ":8334")
	v.SetDefault("transport.a2a.agent_id", "dolphin")
	v.SetDefault("transport.a2a.agent_name", "Dolphin AI Agent")
	v.SetDefault("transport.a2a.agent_version", "0.1.0")
	v.SetDefault("transport.a2a.agent_description", "Cross-terminal/email/chat/SSH AI agent")
	v.SetDefault("transport.a2a.capabilities", []string{"task-execution", "shell-command", "web-search"})
	v.SetDefault("transport.a2a.sync_timeout", "60s")
	v.SetDefault("transport.a2a.handler_path", "/a2a")
	v.SetDefault("transport.a2a.agent_card_path", "/.well-known/agent.json")
	v.SetDefault("transport.a2a.read_header_timeout", 10)
	v.SetDefault("transport.a2a.shutdown_timeout", 5)
	v.SetDefault("transport.a2a.api_key", "")
	v.SetDefault("transport.a2a.tls_enabled", false)
	v.SetDefault("transport.a2a.tls_cert_file", "")
	v.SetDefault("transport.a2a.tls_key_file", "")

	v.SetDefault("session.max_age", "24h")
	v.SetDefault("session.resume", false)

	v.SetDefault("mcp.shell.enabled", true)
	v.SetDefault("mcp.shell.allow_unrestricted", true)
	v.SetDefault("mcp.shell.allowed_commands", []string{"date"})
	v.SetDefault("mcp.shell.timeout_seconds", 30)
	v.SetDefault("mcp.shell.priority", 10)
	v.SetDefault("mcp.shell.max_command_length", 4096)
	v.SetDefault("mcp.shell.output_max_bytes", 65536)
	v.SetDefault("mcp.cdp.enabled", true)
	v.SetDefault("mcp.cdp.headless", true)
	v.SetDefault("mcp.cdp.priority", 1000)
	v.SetDefault("mcp.cdp.idle_timeout", 300)
	v.SetDefault("mcp.cdp.startup_timeout", 30)
	v.SetDefault("mcp.cdp.chrome_flags", map[string]any{
		"disable-gpu":                   true,
		"no-sandbox":                    true,
		"disable-dev-shm-usage":         true,
		"disable-extensions":            true,
		"disable-background-networking": true,
		"disable-sync":                  true,
		"disable-default-apps":          true,
		"disable-translate":             true,
		"no-first-run":                  true,
		"no-default-browser-check":      true,
		"window-size":                   "1920,1080",
		"disable-blink-features":        "AutomationControlled",
	})
	v.SetDefault("mcp.cdp.user_agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36")
	v.SetDefault("mcp.cdp.health_check_timeout", 10)
	v.SetDefault("mcp.cdp.navigation_wait", "2s")
	v.SetDefault("mcp.cdp.screenshot_quality", 100)
	v.SetDefault("mcp.cdp.screenshot_dir", "screenshots")
	v.SetDefault("mcp.email.enabled", true)
	v.SetDefault("mcp.email.priority", 500)
	v.SetDefault("mcp.email.max_attachment_size", 10485760)
	v.SetDefault("mcp.email.connect_timeout", "30s")

	v.SetDefault("mcp.webhook.enabled", true)
	v.SetDefault("mcp.webhook.priority", 100)
	v.SetDefault("mcp.webhook.timeout_seconds", 30)

	v.SetDefault("mcp.web_search.enabled", true)
	v.SetDefault("mcp.web_search.priority", 90)
	v.SetDefault("mcp.web_search.provider", "duckduckgo")
	v.SetDefault("mcp.web_search.api_key", "")
	v.SetDefault("mcp.web_search.timeout_seconds", 15)
	v.SetDefault("mcp.web_search.max_results", 10)
	v.SetDefault("mcp.web_search.provider_base_urls", map[string]string{})

	v.SetDefault("mcp.a2a.enabled", true)
	v.SetDefault("mcp.a2a.timeout_seconds", 30)
	v.SetDefault("mcp.a2a.default_rpc_path", "/rpc")

	v.SetDefault("agent_pool.max_concurrency", 5)
	v.SetDefault("agent_pool.default_timeout", 300)
	v.SetDefault("agent_pool.workspace_dir", ".dolphin/workspaces")
	v.SetDefault("agent_pool.idle_timeout", 600)
	v.SetDefault("agent_pool.max_pending_results", 10)
	v.SetDefault("agent_pool.max_synthesis_rounds", 3)
	v.SetDefault("agent_pool.poll_interval", "200ms")
	v.SetDefault("agent_pool.min_reap_interval", "5s")
	v.SetDefault("agent_pool.max_reap_interval", "30s")
	v.SetDefault("agent_pool.dispatch_timeout", "5s")
	v.SetDefault("agent_pool.worker_stop_timeout", "5s")
	v.SetDefault("agent_pool.max_stale_duration", "1h")
	v.SetDefault("agent_pool.enable_agent_log", false)
	v.SetDefault("agent_pool.max_agent_messages", 100)

	v.SetDefault("skills.dir", ".dolphin/skills")
	v.SetDefault("skills.max_top", 10)
	v.SetDefault("skills.repos", []string{})

	v.SetDefault("workflows.dir", ".dolphin/workflows")

	v.SetDefault("crontab.file", ".dolphin/CRONTAB.md")
	v.SetDefault("crontab.check_interval", "30s")

	v.SetDefault("pprof.enabled", false)
	v.SetDefault("pprof.addr", "127.0.0.1:6060")

	v.SetDefault("diary.dir", ".dolphin/diary")
	v.SetDefault("diary.max_day_sessions", 200)
	v.SetDefault("diary.max_week_days", 7)
	v.SetDefault("diary.max_month_weeks", 5)
	v.SetDefault("diary.max_year_months", 12)
	v.SetDefault("diary.max_total_mb", 500)

	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.addr", "127.0.0.1:9090")

	v.SetDefault("health.enabled", false)
	v.SetDefault("health.addr", "127.0.0.1:9091")

	v.SetDefault("telemetry.enabled", false)
	v.SetDefault("telemetry.service_name", "dolphin")
	v.SetDefault("telemetry.exporter", "stdout")
	v.SetDefault("telemetry.otlp_endpoint", "localhost:4317")
	v.SetDefault("telemetry.otlp_headers", map[string]string{})
	v.SetDefault("telemetry.sample_rate", 1.0)
	v.SetDefault("telemetry.logs_enabled", false)
	v.SetDefault("telemetry.metrics_enabled", false)

	v.SetDefault("flags.self_evolution", false)

	v.SetDefault("resource.enabled", false)
	v.SetDefault("resource.interval", "30s")
	v.SetDefault("resource.disk_paths", []string{"/"})
	v.SetDefault("resource.max_bandwidth", 125000000)
	v.SetDefault("resource.thresholds", []float64{20, 40, 60, 80})

	v.SetDefault("health.debounce", "30s")

	v.SetDefault("sync_config", true)

	v.SetDefault("update.enabled", true)
	v.SetDefault("update.check_interval", "24h")
	v.SetDefault("update.channel", "stable")
	v.SetDefault("update.auto_install", false)
	v.SetDefault("update.timeout_seconds", 30)

	v.SetDefault("log_level", "info")
	v.SetDefault("log_file", ".dolphin/logs/agent.log")
	v.SetDefault("log_max_size", 100)
	v.SetDefault("log_max_age", 30)
	v.SetDefault("log_max_backup", 3)

	v.SetDefault("plugins.enabled", true)
	v.SetDefault("plugins.dir", "~/.dolphin/plugins/")
	v.SetDefault("plugins.webhook_url", "")
	v.SetDefault("plugins.heartbeat_turns", 0)
	v.SetDefault("plugins.script_timeout_seconds", 3)
	v.SetDefault("plugins.webhook_events", []string{"*"})
}
