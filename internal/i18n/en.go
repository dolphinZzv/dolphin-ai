package i18n

var enMessages = map[string]string{
	KeyWelcome:           "Welcome to dolphin — AI Agent",
	KeySelectDomain:      "What kind of work are you doing? Pick one or more (comma-separated, e.g. 1,3,5)",
	KeyPrivacyNote:       "Your choice stays local — nothing is sent anywhere.",
	KeySkip:              "Skip for now",
	KeyChoice:            "Choice",
	KeyRecommendedTools:  "Recommended tools",
	KeySkills:            "Skills",
	KeyMCP:               "MCP",
	KeyInstallHint:       "To install, add tools to your skill/MCP repos or config.",
	KeyToolsInstalled:    "Tools installed. Skills and MCP servers are available immediately.",
	KeyConfigRestrictive: "restrictive (recommended for security)",
	KeyRestrictiveHint:   "Your config has security hardening applied. Change any setting manually and restart.",
	KeySetupHint:         "You can re-run setup anytime with: dolphin setup",
	KeyNoMatch:           "No matching option found. Skipping tool recommendation.\nYou can add tools later in ~/.dolphin/config.yaml",
	KeySystemMDPrompt:    "Auto-generate SYSTEM.md with your system info (OS, shell, CPU count)?",
	KeySystemMDExplain:   "It'll be injected into every session to help the agent understand your environment.",
	KeySystemMDYes:       "yes",
	KeySystemMDNo:        "skip",
	KeySystemMDSkipped:   "Skipped. You can create ~/.dolphin/SYSTEM.md manually later.",
	KeySystemMDGenerated: "Generated",
	KeySystemMDContent:   "System Environment",
	KeyConfigPrompt:      "Auto-generate .dolphin/config.yaml with all settings and comments?",
	KeyConfigExplain:     "It creates a config file you can edit to customize transports, tools, LLM, and more.",
	KeyConfigYes:         "yes",
	KeyConfigNo:          "skip",
	KeyConfigSkipped:     "Skipped. Defaults will be used. You can create .dolphin/config.yaml manually later.",
	KeyConfigGenerated:   "Config generated",

	// Coordinator interaction
	KeyCoordReady:          "dolphin Coordinator Ready\n  /exit           Quit\n  /help           Show help\n  /status         Show status\n  /agents         List agents & status\n  /skills         List skills\n  /commands       User-defined commands\n  /crontab        View scheduled tasks\n  /sessions       List past sessions\n  /mcp            List MCP tools\n  /model [name]   List or switch LLM provider\n  /reload         Reload (restart) the agent\n",
	KeyHelpHeader:          "Commands:",
	KeyHelpExit:            "  /exit          - Exit",
	KeyHelpHelp:            "  /help         - This help",
	KeyHelpAgents:          "  /agents       - List available agents and their status",
	KeyHelpSkills:          "  /skills       - List available skills  |  /skills new <name> - Create skill template | /skills show <name> - Show skill content | /skills delete <name> - Delete a skill",
	KeyHelpCommands:        "  /commands     - List user-defined commands  |  /commands new <name> - Create command template | /commands delete <name> - Delete a command",
	KeyHelpCancel:          "  /cancel       - Cancel all running tasks",
	KeyHelpCancelID:        "  /cancel <id>  - Cancel a specific task by ID",
	KeyHelpMCP:             "  /mcp          - List all MCP tools",
	KeyHelpStatus:          "  /status       - Show current status",
	KeyHelpSessions:        "  /sessions     - List past sessions",
	KeyHelpCron:            "  /crontab      - View scheduled tasks",
	KeyHelpModel:           "  /model [name] - List or switch LLM provider",
	KeyHelpTopMCP:          "Top MCP tools (by usage, use search_mcp_tools to find more):",
	KeyHelpSkillsAvail:     "\nSkills: %d available (use /skills to list, search_skills to find)",
	KeyNoAgents:            "No agents configured.",
	KeyNoAgentsHint:        "Create agents in .dolphin/agents/<name>/agent.yaml",
	KeyAgentHeader:         "%-16s %-10s %-6s %s",
	KeySkillsNotAvail:      "Skills system not available.",
	KeyNoSkills:            "No skills found.",
	KeyNoSkillsHint:        "Add .md files to .dolphin/skills/",
	KeySkillHeader:         "%-20s %-8s %s",
	KeySkillSearchHint:     "Use search_skills to find skills, load_skill to load one.",
	KeySkillNewCreated:     "Created skill %q in %s. Edit the file to customize it.",
	KeySkillNewError:       "Failed to create skill: %v",
	KeySkillNewUsage:       "Usage: /skills new <name>  — creates a skill template in the skills directory",
	KeySkillDeleteUsage:    "  /skills delete <name>  - Delete a skill",
	KeySkillDeleteDone:     "Deleted skill %q.",
	KeySkillDeleteFail:     "Failed to delete skill %q: %v",
	KeySkillShowUsage:      "  /skills show <name>    - Show skill content",
	KeySkillShowFail:       "Skill %q not found.",
	KeySkillShowHeader:     "--- %s ---",
	KeyCommandsNotAvail:    "Commands system not available.",
	KeyNoCommands:          "No user-defined commands.",
	KeyNoCommandsHint:      "Add .md files to .dolphin/commands/",
	KeyCommandHeader:       "%-20s  %s",
	KeyCommandRunHint:      "Type /<command> to run a command, optionally with arguments.",
	KeyCmdNewUsage:         "Usage: /commands new <name> [description]  — creates a command template",
	KeyCmdNewCreated:       "Created command %q in %s. Edit the file to customize it.",
	KeyCmdNewError:         "Failed to create command: %v",
	KeyCmdDeleteUsage:      "  /commands delete <name>  - Delete a command",
	KeyCmdDeleteDone:       "Deleted command %q.",
	KeyCmdDeleteFail:       "Failed to delete command %q: %v",
	KeyCmdShowUsage:        "  /commands show <name>    - Show command content",
	KeyCmdShowFail:         "Command %q not found.",
	KeyCmdShowHeader:       "--- %s ---",
	KeyCronNotAvail:        "Cron scheduler not available.",
	KeyNoCronTasks:         "No scheduled tasks.",
	KeyNoCronHint:          "Use the add_cron_task tool to create one.",
	KeyCronHeader:          "%-20s %-12s %s",
	KeyCronRecent:          "Recent results:",
	KeyResumePrompt:        "\nFound previous session %s (%d turns, %s ago). Resume? [Y/n]: ",
	KeyResumeYes:           "yes",
	KeyCancelAll:           "All running tasks cancelled.",
	KeyCancelTask:          "Task %s cancelled.",
	KeyCancelNotFound:      "No running task found with ID: %s",
	KeySessionCheckpoint:   "\n[Session checkpoint: summary saved, continuing...]\n",
	KeyTurnError:           "\n[Error: %v]",
	KeyNoAvailableProvider: "→ No available LLM provider — check your config\n  See: https://github.com/dolphinZzv/dolphin/blob/main/docs/en/INSTALL.md\n",

	// LLM warnings
	KeyWarnNoLLM:        "\n⚠  LLM not configured — no API key found.\n",
	KeyWarnDefaultModel: "   Default model: %s (base_url: %s)\n",
	KeyWarnSetAPIKey:    "   Set DZ_LLM_API_KEY environment variable or add api_key to config.\n",
	KeyWarnRunSetup:     "   Run:  dolphin setup\n\n",

	// Provider banner
	KeyLLMProvidersHeader: "\nLLM Providers:\n",
	KeyLLMProviderOK:      "  ✓ %s (%s) — %dms\n",
	KeyLLMProviderFail:    "  ✗ %s (%s) — %dms %s\n",
	KeyLLMUsing:           "→ Using: %s\n",

	// /status command
	KeyStatusHeader:   "Status:",
	KeyStatusProvider: "  Provider:    %s",
	KeyStatusModel:    "  Model:       %s",
	KeyStatusSession:  "  Session:     %s (%d turns)",
	KeyStatusAgents:   "  Agents:      %d (%d busy)",
	KeyStatusMCPTools: "  MCP tools:   %d",
	KeyStatusSkills:   "  Skills:      %d",
	KeyStatusCommands: "  Commands:    %d",
	KeyStatusCron:     "  Cron tasks:  %d",
	KeyStatusMemory:   "  Memory:      %d MB",
	KeyNoSession:      "  Session:     none",

	// /sessions command
	KeySessionsHeader: "Sessions (%d):",
	KeyNoSessions:     "No past sessions found.",
	KeySessionRow:     "  %s  %4d turns  %s  in=%d out=%d",

	// /context
	KeyHelpReload:        "  /reload       - Reload (restart) the agent",
	KeyHelpConfig:        "  /config       - List all config settings  |  /config get <path>  |  /config set <path> <value>",
	KeyHelpContext:       "  /context       - Show current context summary; /context <name> to view section",
	KeyContextSummaryHd:  "=== Context Summary ===",
	KeyContextSectionNF:  "Section %q not found.",
	KeyContextSectionHd:  "=== %s ===",
	KeyContextProvider:   "Provider:     %s (%s)",
	KeyContextConfigPath: "Config Paths: %d total",
	KeyContextMCPTools:   "MCP Tools:    %d registered",
	KeyContextAgents:     "Agents:       %d (%d busy)",
	KeyContextSkills:     "Skills:       %d available",
	KeyContextSkillsNA:   "Skills:       not available",
	KeyContextCommands:   "Commands:     %d available",
	KeyContextCommandsNA: "Commands:     not available",
	KeyContextCron:       "Cron Tasks:   %d scheduled",
	KeyContextSelfEvolve: "Self-Evolve:  %v",
	KeyContextSectionsHd: "--- Context Sections (priority · size · path) ---",
	KeyWelcomeBanner:     "dolphin — AI Agent",

	// pprof
	KeyPprofBanner: "\n=== pprof server on %s ===\n",
	KeyPprofURL:    "  http://%s/debug/pprof/\n",
	KeyMetricsBanner: "\n=== Metrics server on %s ===\n",
	KeyMetricsURL:    "  http://%s/metrics\n",

	// Cobra command descriptions
	KeyCmdDolphinUse:   "dolphin",
	KeyCmdDolphinShort: "AI Agent — stdio / SSH / MQTT / Email transport, MCP tools (shell + cdp)",
	KeyCmdDolphinLong: `dolphin is an AI Agent with MCP tool support.

Transports: stdio (default), SSH (:2222), MQTT, Email
Tools: shell, cdp (browser automation)
Config: .dolphin/config.yaml > ~/.dolphin/ > /etc/dolphin/
Env: DZ_LLM_API_KEY, DZ_LLM_MODEL, DZ_LLM_BASE_URL`,

	KeyCmdCompletionUse:   "completion [bash|zsh|fish|powershell]",
	KeyCmdCompletionShort: "Generate shell completion script",
	KeyCmdCompletionLong: `Generate shell completion script for dolphin commands.

Output the completion script for the specified shell.
Source the output to enable tab completion.

  bash:       source <(dolphin completion bash)
  zsh:        source <(dolphin completion zsh)
  fish:       dolphin completion fish | source
  powershell: dolphin completion powershell | Out-String | Invoke-Expression

To make it permanent (bash):
  dolphin completion bash > /etc/bash_completion.d/dolphin

To make it permanent (zsh):
  dolphin completion zsh > "${fpath[1]}/_dolphin"`,

	KeyCmdConfigUse:       "config",
	KeyCmdConfigShort:     "Manage configuration",
	KeyCmdConfigShowUse:   "show",
	KeyCmdConfigShowShort: "Show effective configuration",

	KeyCmdDoctorUse:   "doctor",
	KeyCmdDoctorShort: "Run self-diagnosis checks",
	KeyCmdDoctorLong: `Run self-diagnosis checks on the system to identify configuration issues.

Checks performed:
  - Config file locations and parseability
  - LLM API key presence and endpoint connectivity
  - Session directory accessibility
  - Transport configuration consistency
  - SSH host key availability
  - Skills and MCP directories
  - Port availability for enabled transports`,

	KeyCmdInitUse:   "init",
	KeyCmdInitShort: "Generate a default config file",
	KeyCmdInitLong: `Generates a commented .dolphin/config.yaml with default settings.

Use --restrictive to generate a security-hardened config with:
  - Shell commands restricted to a safe allowlist
  - CDP browser automation disabled
  - Webhook tool disabled
  - Log level set to warn
  - Plugins disabled`,

	KeyCmdNewUse:   "new",
	KeyCmdNewShort: "Start a fresh dolphin session from a clean state",
	KeyCmdNewLong: `Cleans all dolphin runtime data and state, then starts a brand new
dolphin agent session.

Removed:
  - All runtime data (sessions, diary, logs, workspaces, crontab)
  - SSH auto-generated password
  - Cached tool manifests
  - Downloaded skills and commands
  - SYSTEM.md (system prompt)
  - /etc/dolphin/ system-level data
  - First-run marker

Config files (config.yaml) are preserved.`,

	KeyCmdResetUse:   "reset",
	KeyCmdResetShort: "Reset dolphin to a clean state",
	KeyCmdResetLong: `Removes all runtime data, auto-generated files, and the first-run marker
so the next startup feels like the first time.

Runtime data removed:
  - Sessions, diary, logs, workspaces, crontab
  - SSH auto-generated password
  - Cached tool manifests
  - Downloaded skills and commands
  - SYSTEM.md (system prompt)
  - /etc/dolphin/ system-level config and data
  - First-run marker (setup wizard will show on next start)
  - Email-configured marker (startup email sent again on next email session)

Config files (config.yaml) are preserved.`,

	KeyCmdSessionsUse:       "sessions",
	KeyCmdSessionsShort:     "List and manage agent sessions",
	KeyCmdSessionsShowUse:   "show <id>",
	KeyCmdSessionsShowShort: "Show session details as a readable conversation",
	KeyCmdSessionsLogUse:    "log <id>",
	KeyCmdSessionsLogShort:  "Show raw session event log",
	KeyCmdSessionsRmUse:     "rm <id>",
	KeyCmdSessionsRmShort:   "Remove a session file",
	KeyCmdSessionsDumpUse:   "dump <id>",
	KeyCmdSessionsDumpShort: "Generate Mermaid sequence diagram for a session",

	KeyCmdSetupUse:   "setup",
	KeyCmdSetupShort: "Re-run the career-guided tool setup wizard",
	KeyCmdSetupLong: `Re-runs the career selection prompt and displays recommended tools.

The first-run marker is NOT reset, so this does not trigger on next startup.
Use --reset to clear the first-run marker and start fresh.`,

	KeyCmdSkillsUse:          "skills",
	KeyCmdSkillsShort:        "List and manage skills",
	KeyCmdSkillsListUse:      "list",
	KeyCmdSkillsListShort:    "List all installed skills",
	KeyCmdSkillsSearchUse:    "search <query>",
	KeyCmdSkillsSearchShort:  "Search skills by name or description",
	KeyCmdSkillsInstallUse:   "install <name> [description]",
	KeyCmdSkillsInstallShort: "Install a new skill from boilerplate template",
	KeyCmdSkillsDisableUse:   "disable <name>",
	KeyCmdSkillsDisableShort: "Disable and remove a skill",

	KeyCmdStatusUse:   "status",
	KeyCmdStatusShort: "Show dolphin daemon health and configuration status",

	KeyCmdUpdateUse:   "update [version]",
	KeyCmdUpdateShort: "Update dolphin to the latest or specified version from GitHub",
	KeyCmdUpdateLong: `Downloads and installs the specified version of dolphin from GitHub releases.

If no version tag is given, the latest release is used.
The version tag should match a GitHub release tag (e.g. "v1.0.0").

Examples:
  dolphin update          Update to the latest release
  dolphin update v1.0.0   Update to a specific version`,

	KeyCmdVersionUse:   "version",
	KeyCmdVersionShort: "Print the version number",

	KeyCmdConfigFlag: "path to config file (searches .dolphin/, ~/.dolphin/, /etc/dolphin/ by default)",

	// Root command flags
	KeyFlagConfig:  "path to config file (searches .dolphin/, ~/.dolphin/, /etc/dolphin/ by default)",
	KeyFlagVerbose: "enable debug-level logging",
	KeyFlagQuiet:   "suppress non-error output",

	// Doctor output
	KeyDoctorBanner:              "Dolphin Doctor",
	KeyDoctorSep:                 "==============",
	KeyDoctorOK:                  "  [OK]   %s: %s",
	KeyDoctorWarn:                "  [WARN] %s: %s",
	KeyDoctorFail:                "  [FAIL] %s: %s",
	KeyDoctorResults:             "Results: %d pass, %d warn, %d fail",
	KeyDoctorFixHint:             "Run 'dolphin setup' to fix configuration issues.",
	KeyDoctorCfgValid:            "config loaded and validated",
	KeyDoctorCfgFail:             "config.Load failed: %v",
	KeyDoctorLLMKeyOK:            "configured",
	KeyDoctorLLMKeyFail:          "no API key found — set DZ_LLM_API_KEY env var or run 'dolphin setup'",
	KeyDoctorLLMProvFail:         "no providers configured",
	KeyDoctorLLMProvNone:         "%s unreachable: %v (check network or proxy)",
	KeyDoctorLLMBaseEmpty:        "base URL is empty",
	KeyDoctorLLMReachable:        "%s reachable",
	KeyDoctorLLMUnreachable:      "%s unreachable: %v (check network or proxy)",
	KeyDoctorSessOK:              "session directory is writable",
	KeyDoctorSessNotExist:        "%s does not exist (will be created on first run)",
	KeyDoctorSessFail:            "%s: %v",
	KeyDoctorSessNotDir:          "%s is not a directory",
	KeyDoctorSessNotWritable:     "%s is not writable: %v",
	KeyDoctorTransStdio:          "enabled",
	KeyDoctorTransSSH:            "enabled on %s (user: %s)",
	KeyDoctorTransMQTT:           "enabled (broker: %s)",
	KeyDoctorTransEmail:          "enabled (from: %s)",
	KeyDoctorTransNone:           "no transport enabled — enable at least one (stdio, ssh, mqtt, or email)",
	KeyDoctorSSHPassFail:         "SSH password is empty — will be auto-generated, check logs",
	KeyDoctorSSHKeyFail:          "cannot expand ~: %v",
	KeyDoctorSSHKeyWarn:          "no host key at %s or %s — will auto-generate ephemeral key",
	KeyDoctorSSHKeyOK:            "host key found",
	KeyDoctorSSHKeyAuto:          "auto-generated key at %s",
	KeyDoctorSkillsDirOK:         "skills directory is accessible",
	KeyDoctorSkillsDirWarn:       "%s does not exist (will be created on first run)",
	KeyDoctorSkillsDirFail:       "%s: %v",
	KeyDoctorSkillsDirNotDir:     "%s is not a directory",
	KeyDoctorShellDisabled:       "shell tool is disabled (mcp.shell.enabled=false)",
	KeyDoctorShellUnrestricted:   "unrestricted mode — any shell command is allowed",
	KeyDoctorShellRestricted:     "restricted to: %v",
	KeyDoctorShellDefault:        "enabled with default restrictions",
	KeyDoctorPortAvail:           "%s available",
	KeyDoctorPortInUse:           "%s in use or unavailable (%v)",
	KeyDoctorCfgNotFound:         "not found (%s), skipping",
	KeyDoctorCfgUnreadable:       "unreadable: %v",
	KeyDoctorCheckNameCfgSys:     "system config",
	KeyDoctorCheckNameCfgUser:    "user config",
	KeyDoctorCheckNameCfgProj:    "project config",
	KeyDoctorCheckNameCfgVal:     "config validation",
	KeyDoctorCheckNameLLMKey:     "LLM API key",
	KeyDoctorCheckNameLLMProv:    "LLM %q reachability",
	KeyDoctorCheckNameSessDir:    "session directory",
	KeyDoctorCheckNameTransStdio: "transport stdio",
	KeyDoctorCheckNameTransSSH:   "transport ssh",
	KeyDoctorCheckNameTransMQTT:  "transport mqtt",
	KeyDoctorCheckNameTransEmail: "transport email",
	KeyDoctorCheckNameTransports: "transports",
	KeyDoctorCheckNameSSHPass:    "SSH password",
	KeyDoctorCheckNameSSHKey:     "SSH host key",
	KeyDoctorCheckNameSkillsDir:  "skills directory",
	KeyDoctorCheckNameShell:      "MCP shell",
	KeyDoctorUnreadable:          "unreadable: %v",

	// Status output
	KeyStatusVersion:           "Version: %s",
	KeyStatusBuild:             "Build: %s",
	KeyStatusLLM:               "LLM:       configured",
	KeyStatusLLMNotCfg:         "LLM:       NOT configured (run 'dolphin setup')",
	KeyStatusHealthUnreach:     "Health:    unreachable (%v)",
	KeyStatusHealthOK:          "Health:    OK — %s",
	KeyStatusHealthDisabled:    "Health:    disabled (set health.enabled=true)",
	KeyStatusMetricsEnabled:    "Metrics:   enabled at %s",
	KeyStatusMetricsDisabled:   "Metrics:   disabled",
	KeyStatusTransports:        "Transports:",
	KeyStatusShell:             "Shell:",
	KeyStatusShellUnrestricted: "Shell:    unrestricted (pipes and redirects enabled)",
	KeyStatusShellRestricted:   "Shell:    restricted (allowed: %v)",
	KeyStatusTransStdio:        "  - stdio: enabled",
	KeyStatusTransSSH:          "  - ssh:   enabled at %s",
	KeyStatusTransMQTT:         "  - mqtt:  enabled (broker: %s)",
	KeyStatusTransEmail:        "  - email: enabled (from: %s)",

	// Update output
	KeyUpdateCurrent:       "Current version: %s",
	KeyUpdatePlatform:      "Platform: %s/%s",
	KeyUpdateRelease:       "Release: %s",
	KeyUpdateAlreadyLatest: "Already at version %s. No update needed.",
	KeyUpdateReady:         "\nReady to download and install %s (%s)",
	KeyUpdateBinary:        "Current binary: %s",
	KeyUpdateConfirm:       "Are you sure? [y/N]: ",
	KeyUpdateCancelled:     "Update cancelled.",
	KeyUpdateDownloading:   "\nDownloading %s ...",
	KeyUpdateComplete:      "\nUpdated to %s",
	KeyUpdateVerify:        "Run 'dolphin --version' to verify.",
	KeyUpdateNoReleases:    "No releases found.",
	KeyUpdateAvailable:     "Available versions (%s/%s):",
	KeyUpdatePreRelease:    "\n⚠ = pre-release",

	// Setup output
	KeySetupFirstRunReset: "First-run marker reset. Career prompt will show on next startup.",
	KeySetupSkipped:       "\nSetup skipped. No changes made.",
	KeySetupRecTools:      "\n=== Recommended tools for %s ===",
	KeySetupLoadTo:        "\nLoad to: [p] project  [a] global  [n] skip",
	KeySetupChoice:        "Choice: ",
	KeySetupSavedProject:  "\nTools saved to .dolphin/config.yaml",
	KeySetupSavedGlobal:   "\nTools saved to ~/.dolphin/config.yaml",
	KeySetupSkipNoChange:  "\nSkipped. No changes made.",
	KeySetupManual:        "You can add tools manually in your config or skill/MCP repos.",
	KeySetupComplete:      "=== Setup Complete ===",
	KeySetupProfile:       "  Profile: %s",
	KeySetupSkillsCount:   "  Skills:  %d",
	KeySetupMCPCount:      "  MCP:     %d",
	KeySetupNextSteps:     "Next steps:",
	KeySetupStep1:         "  1. Set your LLM API key: export DZ_LLM_API_KEY=sk-...",
	KeySetupStep2:         "  2. Restart dolphin for changes to take effect",
	KeySetupStep3:         "  3. Run 'dolphin doctor' to verify your setup",

	// Init output
	KeyInitRestrictiveGenerated: "\nSecurity-hardened config generated: %s",
	KeyInitRestrictiveDiffs:     "\nKey differences from default:",
	KeyInitRestrictiveShell:     "  - Shell: only allowlisted commands (ls, cat, grep, find, ...)",
	KeyInitRestrictiveCDP:       "  - CDP browser: disabled",
	KeyInitRestrictiveWebhook:   "  - Webhook: disabled",
	KeyInitRestrictiveLog:       "  - Log level: warn",
	KeyInitRestrictivePlugins:   "  - Plugins: disabled",
	KeyInitRestrictiveRun:       "\nRun 'dolphin' to start with this config.",
	KeyInitDefaultGenerated:     "Default config generated: %s",
	KeyInitEditAndRun:           "Edit it and run 'dolphin' to start.",
	KeyInitGitError:             "git init .dolphin: %v",
	KeyInitRestrictiveFlag:      "generate security-hardened config (restricted shell, CDP/webhook disabled, warn log level)",

	// Reset output
	KeyResetWillRemove:  "The following will be removed:",
	KeyResetComplete:    "Reset complete: %d items removed",
	KeyResetMarkerReset: "The first-run marker has been reset.",
	KeyResetRunAgain:    "Run 'dolphin' to go through the initial setup wizard again.",
	KeyResetConfirm:     "\nAre you sure? This action cannot be undone. [y/N]: ",
	KeyResetCancelled:   "%s cancelled.",

	// New session output
	KeyNewStarting: "Starting a fresh dolphin session:",

	// Sessions output
	KeySessNoDir:        "No sessions found (directory does not exist)",
	KeySessNone:         "No sessions found.",
	KeySessDirLabel:     "Sessions in: %s",
	KeySessNotFound:     "session %q not found",
	KeySessNoEvents:     "No events in session.",
	KeySessHeader:       "Session: %s",
	KeySessDuration:     "Duration: %s — %s (%d events)",
	KeySessTurnTokens:   " (tokens: %d in / %d out)",
	KeySessRemoved:      "Removed session %q",
	KeySessDumpNoEvents: "no events in session",
	KeySessServing:      "Serving at %s",
	KeySessStopHint:     "Press Ctrl+C to stop.",

	// Config show output
	KeyCfgShowLLM:            "LLM:",
	KeyCfgShowSession:        "Session:",
	KeyCfgShowTransports:     "Transports:",
	KeyCfgShowMCP:            "MCP Tools:",
	KeyCfgShowAgentPool:      "Agent Pool:",
	KeyCfgShowSkills:         "Skills:",
	KeyCfgShowCrontab:        "Crontab:",
	KeyCfgShowMonitoring:     "Monitoring:",
	KeyCfgShowPlugins:        "Plugins:",
	KeyCfgShowLogLevel:       "Log Level: %s",
	KeyCfgShowLogFile:        "Log File:  %s",
	KeyCfgShowEnabled:        "enabled",
	KeyCfgShowDisabled:       "disabled",
	KeyCfgShowType:           "  Type:       %s",
	KeyCfgShowModel:          "  Model:      %s",
	KeyCfgShowBaseURL:        "  Base URL:   %s",
	KeyCfgShowAPIKey:         "  API Key:    %s",
	KeyCfgShowMaxTokens:      "  Max Tokens: %d",
	KeyCfgShowMaxCtxTokens:   "  Max Context Tokens: %d",
	KeyCfgShowTemperature:    "  Temperature: %.1f",
	KeyCfgShowMaxSubTurns:    "  Max Sub-turns: %d",
	KeyCfgShowCompressMode:   "  Compress Mode: %s",
	KeyCfgShowMaxLoop:        "  Max Loop: %d",
	KeyCfgShowSummary:        "  Summary:  %v",
	KeyCfgShowMaxAge:         "  Max Age:  %s",
	KeyCfgShowShell:          "  Shell:   enabled=%v",
	KeyCfgShowCDP:            "  CDP:     enabled=%v",
	KeyCfgShowEmail:          "  Email:   enabled=%v",
	KeyCfgShowRepos:          "  Repos:   %v",
	KeyCfgShowMaxConcurrency: "  Max Concurrency:  %d",
	KeyCfgShowDefaultTimeout: "  Default Timeout:  %ds",
	KeyCfgShowIdleTimeout:    "  Idle Timeout:     %ds",
	KeyCfgShowWorkspace:      "  Workspace:        %s",
	KeyCfgShowMaxPending:     "  Max Pending Results: %d",
	KeyCfgShowDir:            "  Dir:    %s",
	KeyCfgShowMaxTop:         "  Max Top: %d",
	KeyCfgShowFile:           "  File:           %s",
	KeyCfgShowCheckInterval:  "  Check Interval: %s",
	KeyCfgShowRestricted:     " (restricted: %v)",
	KeyCfgShowUnrestricted:   " (unrestricted)",
	KeyCfgShowDefault:        " (default)",
	KeyCfgShowRemote:         " (remote: %s)",
	KeyCfgShowHeadless:       " (headless: %v)",
	KeyCfgShowServer:         "  Server(%s): %s (type: %s)",

	// Transport startup messages
	KeyTransSSHServer:   "\n=== SSH server configured on %s ===\n",
	KeyTransSSHConnect:  "Connect: ssh %s@<host> -p %s",
	KeyTransMQTTActive:  "\n=== MQTT transport active ===\n",
	KeyTransMQTTBroker:  "Broker: %s  Topic: %s  Client: %s",
	KeyTransEmailActive: "\n=== Email transport active ===\n",
	KeyTransEmailIMAP:   "IMAP: %s:%d (poll every %s)",
	KeyTransEmailSMTP:   "SMTP: %s:%d",
	KeyTransEmailHint:   "Send an email to %s — subject = command",
	KeyTransDingTalk:    "\n=== DingTalk bot active (Stream mode) ===\n",
	KeyTransNoneEnabled: "no transport enabled (enable stdio, ssh, mqtt, or email in config)",

	// Common
	KeyEnabled:    "enabled",
	KeyDisabled:   "disabled",
	KeyNotFound:   "not found",
	KeyError:      "error",
	KeySkipped:    "skipped",
	KeyCancelled:  "cancelled",
	KeyAreYouSure: "Are you sure? [y/N]: ",
	KeyYes:        "yes",
	KeyNo:         "no",

	// Skills CLI output
	KeySkillsCLINone:       "No skills installed.",
	KeySkillsCLITotal:      "\nTotal: %d skills",
	KeySkillsCLIInstalled:  "Skill %q installed in %s",
	KeySkillsCLISearchNone: "No skills found matching %q.",
	KeySkillsCLIFound:      "\nFound %d results matching %q (* = installed).",
	KeySkillsCLIEdit:       "Edit the file to add your skill content.",
	KeySkillsCLIDisabled:   "Skill %q disabled and removed.",

	// Cleanup common
	KeyCleanupComplete: "Cleanup complete: %d items removed",
	KeyNotExistSkip:    " (not found, skipped)",
	KeyDirectory:       "/ (directory)",
}
