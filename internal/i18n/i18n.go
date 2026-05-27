// Package i18n provides internationalization support with English and Chinese translations.
package i18n

import (
	"os"
	"strings"
	"sync"
)

// Lang represents a locale.
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

// Message keys
const (
	KeyWelcomeBanner = "welcome_banner"

	KeyChoice = "choice"

	KeySkills         = "skills"
	KeyMCP            = "mcp"
	KeyInstallHint    = "install_hint"
	KeyToolsInstalled = "tools_installed"

	KeySystemMDPrompt    = "system_md_prompt"
	KeySystemMDExplain   = "system_md_explain"
	KeySystemMDYes       = "system_md_yes"
	KeySystemMDNo        = "system_md_no"
	KeySystemMDSkipped   = "system_md_skipped"
	KeySystemMDGenerated = "system_md_generated"
	KeySystemMDContent   = "system_md_content"
	KeyConfigPrompt      = "config_prompt"
	KeyConfigExplain     = "config_explain"
	KeyConfigYes         = "config_yes"
	KeyConfigNo          = "config_no"
	KeyConfigSkipped     = "config_skipped"
	KeyConfigGenerated   = "config_generated"
	KeyConfigRestrictive = "config_restrictive"
	KeyRestrictiveHint   = "restrictive_hint"

	// Coordinator interaction
	KeyCoordReady          = "coord_ready"
	KeyHelpHeader          = "help_header"
	KeyHelpExit            = "help_exit"
	KeyHelpHelp            = "help_help"
	KeyHelpAgents          = "help_agents"
	KeyHelpSkills          = "help_skills"
	KeyHelpCommands        = "help_commands"
	KeyHelpCancel          = "help_cancel"
	KeyHelpCancelID        = "help_cancel_id"
	KeyHelpMCP             = "help_mcp"
	KeyHelpStatus          = "help_status"
	KeyHelpSessions        = "help_sessions"
	KeyHelpCron            = "help_cron"
	KeyHelpModel           = "help_model"
	KeyHelpTopMCP          = "help_top_mcp"
	KeyHelpSkillsAvail     = "help_skills_avail"
	KeyNoAgents            = "no_agents"
	KeyNoAgentsHint        = "no_agents_hint"
	KeyAgentHeader         = "agent_header"
	KeySkillsNotAvail      = "skills_not_avail"
	KeyNoSkills            = "no_skills"
	KeyNoSkillsHint        = "no_skills_hint"
	KeySkillHeader         = "skill_header"
	KeySkillSearchHint     = "skill_search_hint"
	KeySkillNewCreated     = "skill_new_created"
	KeySkillNewError       = "skill_new_error"
	KeySkillNewUsage       = "skill_new_usage"
	KeySkillDeleteUsage    = "skill_delete_usage"
	KeySkillDeleteDone     = "skill_delete_done"
	KeySkillDeleteFail     = "skill_delete_fail"
	KeySkillShowUsage      = "skill_show_usage"
	KeySkillShowFail       = "skill_show_fail"
	KeySkillShowHeader     = "skill_show_header"
	KeyCommandsNotAvail    = "commands_not_avail"
	KeyNoCommands          = "no_commands"
	KeyNoCommandsHint      = "no_commands_hint"
	KeyCommandHeader       = "command_header"
	KeyCommandRunHint      = "command_run_hint"
	KeyCmdNewUsage         = "cmd_new_usage"
	KeyCmdNewCreated       = "cmd_new_created"
	KeyCmdNewError         = "cmd_new_error"
	KeyCmdDeleteUsage      = "cmd_delete_usage"
	KeyCmdDeleteDone       = "cmd_delete_done"
	KeyCmdDeleteFail       = "cmd_delete_fail"
	KeyCmdShowUsage        = "cmd_show_usage"
	KeyCmdShowFail         = "cmd_show_fail"
	KeyCmdShowHeader       = "cmd_show_header"
	KeyCronNotAvail        = "cron_not_avail"
	KeyNoCronTasks         = "no_cron_tasks"
	KeyNoCronHint          = "no_cron_hint"
	KeyCronHeader          = "cron_header"
	KeyCronRecent          = "cron_recent"
	KeyResumePrompt        = "resume_prompt"
	KeyResumeYes           = "resume_yes"
	KeyCancelAll           = "cancel_all"
	KeyCancelTask          = "cancel_task"
	KeyCancelNotFound      = "cancel_not_found"
	KeySessionCheckpoint   = "session_checkpoint"
	KeyTurnError           = "turn_error"
	KeyNoAvailableProvider = "no_available_provider"

	// LLM warnings (root.go)
	KeyWarnNoLLM        = "warn_no_llm"
	KeyWarnDefaultModel = "warn_default_model"
	KeyWarnSetAPIKey    = "warn_set_api_key"
	KeyWarnRunSetup     = "warn_run_setup"

	// Provider banner (loop.go)
	KeyLLMProvidersHeader = "llm_providers_header"
	KeyLLMProviderOK      = "llm_provider_ok"
	KeyLLMProviderFail    = "llm_provider_fail"
	KeyLLMUsing           = "llm_using"

	// /status command
	KeyStatusHeader   = "status_header"
	KeyStatusProvider = "status_provider"
	KeyStatusModel    = "status_model"
	KeyStatusSession  = "status_session"
	KeyStatusAgents   = "status_agents"
	KeyStatusMCPTools = "status_mcp_tools"
	KeyStatusSkills   = "status_skills"
	KeyStatusCommands = "status_commands"
	KeyStatusCron     = "status_cron"
	KeyStatusMemory   = "status_memory"
	KeyStatusLimits   = "status_limits"
	KeyNoSession      = "no_session"

	// /sessions command
	KeySessionsHeader = "sessions_header"
	KeyNoSessions     = "no_sessions"
	KeySessionRow     = "session_row"

	KeyHelpConfig = "help_config"

	// /context
	KeyHelpContext       = "help_context"
	KeyHelpReload        = "help_reload"
	KeyHelpWorkflow      = "help_workflow"
	KeyContextSummaryHd  = "context_summary_hd"
	KeyContextSectionNF  = "context_section_nf"
	KeyContextSectionHd  = "context_section_hd"
	KeyContextProvider   = "context_provider"
	KeyContextConfigPath = "context_config_path"
	KeyContextMCPTools   = "context_mcp_tools"
	KeyContextAgents     = "context_agents"
	KeyContextSkills     = "context_skills"
	KeyContextSkillsNA   = "context_skills_na"
	KeyContextCommands   = "context_commands"
	KeyContextCommandsNA = "context_commands_na"
	KeyContextCron       = "context_cron"
	KeyContextSelfEvolve = "context_self_evolve"
	KeyContextSectionsHd = "context_sections_hd"

	// pprof
	KeyPprofBanner = "pprof_banner"
	KeyPprofURL    = "pprof_url"

	// metrics
	KeyMetricsBanner = "metrics_banner"
	KeyMetricsURL    = "metrics_url"

	// Cobra command descriptions
	KeyCmdDolphinUse        = "cmd_dolphin_use"
	KeyCmdDolphinShort      = "cmd_dolphin_short"
	KeyCmdDolphinLong       = "cmd_dolphin_long"
	KeyCmdCompletionUse     = "cmd_completion_use"
	KeyCmdCompletionShort   = "cmd_completion_short"
	KeyCmdCompletionLong    = "cmd_completion_long"
	KeyCmdConfigUse         = "cmd_config_use"
	KeyCmdConfigShort       = "cmd_config_short"
	KeyCmdConfigShowUse     = "cmd_config_show_use"
	KeyCmdConfigShowShort   = "cmd_config_show_short"
	KeyCmdDoctorUse         = "cmd_doctor_use"
	KeyCmdDoctorShort       = "cmd_doctor_short"
	KeyCmdDoctorLong        = "cmd_doctor_long"
	KeyCmdInitUse           = "cmd_init_use"
	KeyCmdInitShort         = "cmd_init_short"
	KeyCmdInitLong          = "cmd_init_long"
	KeyCmdNewUse            = "cmd_new_use"
	KeyCmdNewShort          = "cmd_new_short"
	KeyCmdNewLong           = "cmd_new_long"
	KeyCmdResetUse          = "cmd_reset_use"
	KeyCmdResetShort        = "cmd_reset_short"
	KeyCmdResetLong         = "cmd_reset_long"
	KeyCmdSessionsUse       = "cmd_sessions_use"
	KeyCmdSessionsShort     = "cmd_sessions_short"
	KeyCmdSessionsShowUse   = "cmd_sessions_show_use"
	KeyCmdSessionsShowShort = "cmd_sessions_show_short"
	KeyCmdSessionsLogUse    = "cmd_sessions_log_use"
	KeyCmdSessionsLogShort  = "cmd_sessions_log_short"
	KeyCmdSessionsRmUse     = "cmd_sessions_rm_use"
	KeyCmdSessionsRmShort   = "cmd_sessions_rm_short"
	KeyCmdSessionsDumpUse   = "cmd_sessions_dump_use"
	KeyCmdSessionsDumpShort = "cmd_sessions_dump_short"

	KeyCmdSkillsUse            = "cmd_skills_use"
	KeyCmdSkillsShort          = "cmd_skills_short"
	KeyCmdSkillsListUse        = "cmd_skills_list_use"
	KeyCmdSkillsListShort      = "cmd_skills_list_short"
	KeyCmdSkillsSearchUse      = "cmd_skills_search_use"
	KeyCmdSkillsSearchShort    = "cmd_skills_search_short"
	KeyCmdSkillsInstallUse     = "cmd_skills_install_use"
	KeyCmdSkillsInstallShort   = "cmd_skills_install_short"
	KeyCmdSkillsNewUse         = "cmd_skills_new_use"
	KeyCmdSkillsNewShort       = "cmd_skills_new_short"
	KeyCmdSkillsDisableUse     = "cmd_skills_disable_use"
	KeyCmdSkillsDisableShort   = "cmd_skills_disable_short"
	KeyCmdSkillsEnableUse      = "cmd_skills_enable_use"
	KeyCmdSkillsEnableShort    = "cmd_skills_enable_short"
	KeyCmdSkillsUninstallUse   = "cmd_skills_uninstall_use"
	KeyCmdSkillsUninstallShort = "cmd_skills_uninstall_short"
	KeyCmdSkillsShowUse        = "cmd_skills_show_use"
	KeyCmdSkillsShowShort      = "cmd_skills_show_short"
	KeyCmdCommandsUse          = "cmd_commands_use"
	KeyCmdCommandsShort        = "cmd_commands_short"
	KeyCmdCommandsListUse      = "cmd_commands_list_use"
	KeyCmdCommandsListShort    = "cmd_commands_list_short"
	KeyCmdCommandsNewUse       = "cmd_commands_new_use"
	KeyCmdCommandsNewShort     = "cmd_commands_new_short"
	KeyCmdCommandsDeleteUse    = "cmd_commands_delete_use"
	KeyCmdCommandsDeleteShort  = "cmd_commands_delete_short"
	KeyCmdCommandsShowUse      = "cmd_commands_show_use"
	KeyCmdCommandsShowShort    = "cmd_commands_show_short"
	KeyCmdAgentUse             = "cmd_agent_use"
	KeyCmdAgentShort           = "cmd_agent_short"
	KeyCmdAgentListUse         = "cmd_agent_list_use"
	KeyCmdAgentListShort       = "cmd_agent_list_short"
	KeyCmdAgentSearchUse       = "cmd_agent_search_use"
	KeyCmdAgentSearchShort     = "cmd_agent_search_short"
	KeyCmdAgentInstallUse      = "cmd_agent_install_use"
	KeyCmdAgentInstallShort    = "cmd_agent_install_short"
	KeyCmdAgentNewUse          = "cmd_agent_new_use"
	KeyCmdAgentNewShort        = "cmd_agent_new_short"
	KeyCmdAgentDisableUse      = "cmd_agent_disable_use"
	KeyCmdAgentDisableShort    = "cmd_agent_disable_short"
	KeyCmdAgentEnableUse       = "cmd_agent_enable_use"
	KeyCmdAgentEnableShort     = "cmd_agent_enable_short"
	KeyCmdAgentUninstallUse    = "cmd_agent_uninstall_use"
	KeyCmdAgentUninstallShort  = "cmd_agent_uninstall_short"
	KeyCmdMCPUse               = "cmd_mcp_use"
	KeyCmdMCPShort             = "cmd_mcp_short"
	KeyCmdMCPSearchUse         = "cmd_mcp_search_use"
	KeyCmdMCPSearchShort       = "cmd_mcp_search_short"
	KeyCmdMCPInstallUse        = "cmd_mcp_install_use"
	KeyCmdMCPInstallShort      = "cmd_mcp_install_short"
	KeyCmdMCPUninstallUse      = "cmd_mcp_uninstall_use"
	KeyCmdMCPUninstallShort    = "cmd_mcp_uninstall_short"
	KeyCmdMCPEnableUse         = "cmd_mcp_enable_use"
	KeyCmdMCPEnableShort       = "cmd_mcp_enable_short"
	KeyCmdMCPDisableUse        = "cmd_mcp_disable_use"
	KeyCmdMCPDisableShort      = "cmd_mcp_disable_short"
	KeyCmdStatusUse            = "cmd_status_use"
	KeyCmdStatusShort          = "cmd_status_short"
	KeyCmdUpdateUse            = "cmd_update_use"
	KeyCmdUpdateShort          = "cmd_update_short"
	KeyCmdUpdateLong           = "cmd_update_long"
	KeyCmdInstallUse           = "cmd_install_use"
	KeyCmdInstallShort         = "cmd_install_short"
	KeyCmdVersionUse           = "cmd_version_use"
	KeyCmdVersionShort         = "cmd_version_short"
	KeyCmdConfigFlag           = "cmd_config_flag"

	// Root command flags
	KeyFlagConfig  = "flag_config"
	KeyFlagVerbose = "flag_verbose"
	KeyFlagQuiet   = "flag_quiet"

	// Doctor output
	KeyDoctorBanner              = "doctor_banner"
	KeyDoctorSep                 = "doctor_sep"
	KeyDoctorOK                  = "doctor_ok"
	KeyDoctorWarn                = "doctor_warn"
	KeyDoctorFail                = "doctor_fail"
	KeyDoctorResults             = "doctor_results"
	KeyDoctorFixHint             = "doctor_fix_hint"
	KeyDoctorCfgValid            = "doctor_cfg_valid"
	KeyDoctorCfgFail             = "doctor_cfg_fail"
	KeyDoctorLLMKeyOK            = "doctor_llm_key_ok"
	KeyDoctorLLMKeyFail          = "doctor_llm_key_fail"
	KeyDoctorLLMProvFail         = "doctor_llm_prov_fail"
	KeyDoctorLLMProvNone         = "doctor_llm_prov_none"
	KeyDoctorLLMBaseEmpty        = "doctor_llm_base_empty"
	KeyDoctorLLMReachable        = "doctor_llm_reachable"
	KeyDoctorLLMUnreachable      = "doctor_llm_unreachable"
	KeyDoctorSessOK              = "doctor_sess_ok"
	KeyDoctorSessNotExist        = "doctor_sess_not_exist"
	KeyDoctorSessFail            = "doctor_sess_fail"
	KeyDoctorSessNotDir          = "doctor_sess_not_dir"
	KeyDoctorSessNotWritable     = "doctor_sess_not_writable"
	KeyDoctorTransStdio          = "doctor_trans_stdio"
	KeyDoctorTransSSH            = "doctor_trans_ssh"
	KeyDoctorTransMQTT           = "doctor_trans_mqtt"
	KeyDoctorTransEmail          = "doctor_trans_email"
	KeyDoctorTransNone           = "doctor_trans_none"
	KeyDoctorSSHPassFail         = "doctor_ssh_pass_fail"
	KeyDoctorSSHKeyFail          = "doctor_ssh_key_fail"
	KeyDoctorSSHKeyWarn          = "doctor_ssh_key_warn"
	KeyDoctorSSHKeyOK            = "doctor_ssh_key_ok"
	KeyDoctorSSHKeyAuto          = "doctor_ssh_key_auto"
	KeyDoctorSkillsDirOK         = "doctor_skills_dir_ok"
	KeyDoctorSkillsDirWarn       = "doctor_skills_dir_warn"
	KeyDoctorSkillsDirFail       = "doctor_skills_dir_fail"
	KeyDoctorSkillsDirNotDir     = "doctor_skills_dir_not_dir"
	KeyDoctorShellDisabled       = "doctor_shell_disabled"
	KeyDoctorShellUnrestricted   = "doctor_shell_unrestricted"
	KeyDoctorShellRestricted     = "doctor_shell_restricted"
	KeyDoctorShellDefault        = "doctor_shell_default"
	KeyDoctorPortAvail           = "doctor_port_avail"
	KeyDoctorPortInUse           = "doctor_port_in_use"
	KeyDoctorCfgNotFound         = "doctor_cfg_not_found"
	KeyDoctorCfgUnreadable       = "doctor_cfg_unreadable"
	KeyDoctorCheckNameCfgSys     = "doctor_check_cfg_sys"
	KeyDoctorCheckNameCfgUser    = "doctor_check_cfg_user"
	KeyDoctorCheckNameCfgProj    = "doctor_check_cfg_proj"
	KeyDoctorCheckNameCfgVal     = "doctor_check_cfg_val"
	KeyDoctorCheckNameLLMKey     = "doctor_check_llm_key"
	KeyDoctorCheckNameLLMProv    = "doctor_check_llm_prov"
	KeyDoctorCheckNameSessDir    = "doctor_check_sess_dir"
	KeyDoctorCheckNameTransStdio = "doctor_check_trans_stdio"
	KeyDoctorCheckNameTransSSH   = "doctor_check_trans_ssh"
	KeyDoctorCheckNameTransMQTT  = "doctor_check_trans_mqtt"
	KeyDoctorCheckNameTransEmail = "doctor_check_trans_email"
	KeyDoctorCheckNameTransports = "doctor_check_transports"
	KeyDoctorCheckNameSSHPass    = "doctor_check_ssh_pass"
	KeyDoctorCheckNameSSHKey     = "doctor_check_ssh_key"
	KeyDoctorCheckNameSkillsDir  = "doctor_check_skills_dir"
	KeyDoctorCheckNameShell      = "doctor_check_shell"
	KeyDoctorUnreadable          = "doctor_unreadable"

	// Status output
	KeyStatusVersion           = "status_version"
	KeyStatusBuild             = "status_build"
	KeyStatusLLM               = "status_llm"
	KeyStatusLLMNotCfg         = "status_llm_not_cfg"
	KeyStatusHealthUnreach     = "status_health_unreach"
	KeyStatusHealthOK          = "status_health_ok"
	KeyStatusHealthDisabled    = "status_health_disabled"
	KeyStatusMetricsEnabled    = "status_metrics_enabled"
	KeyStatusMetricsDisabled   = "status_metrics_disabled"
	KeyStatusTransports        = "status_transports"
	KeyStatusShell             = "status_shell"
	KeyStatusShellUnrestricted = "status_shell_unrestricted"
	KeyStatusShellRestricted   = "status_shell_restricted"
	KeyStatusTransStdio        = "status_trans_stdio"
	KeyStatusTransSSH          = "status_trans_ssh"
	KeyStatusTransMQTT         = "status_trans_mqtt"
	KeyStatusTransEmail        = "status_trans_email"

	// Update output
	KeyUpdateCurrent       = "update_current"
	KeyUpdatePlatform      = "update_platform"
	KeyUpdateRelease       = "update_release"
	KeyUpdateAlreadyLatest = "update_already_latest"
	KeyUpdateReady         = "update_ready"
	KeyUpdateBinary        = "update_binary"
	KeyUpdateConfirm       = "update_confirm"
	KeyUpdateCancelled     = "update_cancelled"
	KeyUpdateDownloading   = "update_downloading"
	KeyUpdateComplete      = "update_complete"
	KeyUpdateVerify        = "update_verify"
	KeyUpdateNoReleases    = "update_no_releases"
	KeyUpdateAvailable     = "update_available"
	KeyUpdatePreRelease    = "update_pre_release"

	// Setup output

	// Init output
	KeyInitRestrictiveGenerated = "init_restrictive_generated"
	KeyInitRestrictiveDiffs     = "init_restrictive_diffs"
	KeyInitRestrictiveShell     = "init_restrictive_shell"
	KeyInitRestrictiveCDP       = "init_restrictive_cdp"
	KeyInitRestrictiveWebhook   = "init_restrictive_webhook"
	KeyInitRestrictiveLog       = "init_restrictive_log"
	KeyInitRestrictivePlugins   = "init_restrictive_plugins"
	KeyInitRestrictiveRun       = "init_restrictive_run"
	KeyInitDefaultGenerated     = "init_default_generated"
	KeyInitEditAndRun           = "init_edit_and_run"
	KeyInitGitError             = "init_git_error"
	KeyInitRestrictiveFlag      = "init_restrictive_flag"

	// Reset output
	KeyResetWillRemove  = "reset_will_remove"
	KeyResetComplete    = "reset_complete"
	KeyResetMarkerReset = "reset_marker_reset"
	KeyResetRunAgain    = "reset_run_again"
	KeyResetConfirm     = "reset_confirm"
	KeyResetCancelled   = "reset_cancelled"

	// New session output
	KeyNewStarting = "new_starting"

	// Sessions output
	KeySessNoDir        = "sess_no_dir"
	KeySessNone         = "sess_none"
	KeySessDirLabel     = "sess_dir_label"
	KeySessNotFound     = "sess_not_found"
	KeySessNoEvents     = "sess_no_events"
	KeySessHeader       = "sess_header"
	KeySessDuration     = "sess_duration"
	KeySessTurnTokens   = "sess_turn_tokens"
	KeySessRemoved      = "sess_removed"
	KeySessDumpNoEvents = "sess_dump_no_events"
	KeySessServing      = "sess_serving"
	KeySessStopHint     = "sess_stop_hint"

	// Config show output
	KeyCfgShowLLM            = "cfg_show_llm"
	KeyCfgShowSession        = "cfg_show_session"
	KeyCfgShowTransports     = "cfg_show_transports"
	KeyCfgShowMCP            = "cfg_show_mcp"
	KeyCfgShowAgentPool      = "cfg_show_agent_pool"
	KeyCfgShowSkills         = "cfg_show_skills"
	KeyCfgShowCrontab        = "cfg_show_crontab"
	KeyCfgShowMonitoring     = "cfg_show_monitoring"
	KeyCfgShowPlugins        = "cfg_show_plugins"
	KeyCfgShowLogLevel       = "cfg_show_log_level"
	KeyCfgShowLogFile        = "cfg_show_log_file"
	KeyCfgShowEnabled        = "cfg_show_enabled"
	KeyCfgShowDisabled       = "cfg_show_disabled"
	KeyCfgShowType           = "cfg_show_type"
	KeyCfgShowModel          = "cfg_show_model"
	KeyCfgShowBaseURL        = "cfg_show_base_url"
	KeyCfgShowAPIKey         = "cfg_show_api_key"
	KeyCfgShowMaxTokens      = "cfg_show_max_tokens"
	KeyCfgShowMaxCtxTokens   = "cfg_show_max_ctx_tokens"
	KeyCfgShowTemperature    = "cfg_show_temperature"
	KeyCfgShowMaxSubTurns    = "cfg_show_max_sub_turns"
	KeyCfgShowCompressMode   = "cfg_show_compress_mode"
	KeyCfgShowMaxLoop        = "cfg_show_max_loop"
	KeyCfgShowSummary        = "cfg_show_summary"
	KeyCfgShowMaxAge         = "cfg_show_max_age"
	KeyCfgShowShell          = "cfg_show_shell"
	KeyCfgShowCDP            = "cfg_show_cdp"
	KeyCfgShowEmail          = "cfg_show_email"
	KeyCfgShowRepos          = "cfg_show_repos"
	KeyCfgShowMaxConcurrency = "cfg_show_max_concurrency"
	KeyCfgShowDefaultTimeout = "cfg_show_default_timeout"
	KeyCfgShowIdleTimeout    = "cfg_show_idle_timeout"
	KeyCfgShowWorkspace      = "cfg_show_workspace"
	KeyCfgShowMaxPending     = "cfg_show_max_pending"
	KeyCfgShowDir            = "cfg_show_dir"
	KeyCfgShowMaxTop         = "cfg_show_max_top"
	KeyCfgShowFile           = "cfg_show_file"
	KeyCfgShowCheckInterval  = "cfg_show_check_interval"
	KeyCfgShowRestricted     = "cfg_show_restricted"
	KeyCfgShowUnrestricted   = "cfg_show_unrestricted"
	KeyCfgShowDefault        = "cfg_show_default"
	KeyCfgShowRemote         = "cfg_show_remote"
	KeyCfgShowHeadless       = "cfg_show_headless"
	KeyCfgShowServer         = "cfg_show_server"

	// Transport startup messages
	KeyTransSSHServer   = "trans_ssh_server"
	KeyTransSSHConnect  = "trans_ssh_connect"
	KeyTransMQTTActive  = "trans_mqtt_active"
	KeyTransMQTTBroker  = "trans_mqtt_broker"
	KeyTransEmailActive = "trans_email_active"
	KeyTransEmailIMAP   = "trans_email_imap"
	KeyTransEmailSMTP   = "trans_email_smtp"
	KeyTransEmailHint   = "trans_email_hint"
	KeyTransDingTalk    = "trans_dingtalk"
	KeyTransA2AActive   = "trans_a2a_active"
	KeyTransNoneEnabled = "trans_none_enabled"

	// Common
	KeyEnabled    = "enabled"
	KeyDisabled   = "disabled"
	KeyNotFound   = "not_found"
	KeyError      = "error"
	KeySkipped    = "skipped"
	KeyCancelled  = "cancelled"
	KeyAreYouSure = "are_you_sure"
	KeyYes        = "yes"
	KeyNo         = "no"

	// Skills CLI output
	KeySkillsCLINone        = "skills_cli_none"
	KeySkillsCLITotal       = "skills_cli_total"
	KeySkillsCLIInstalled   = "skills_cli_installed"
	KeySkillsCLISearchNone  = "skills_cli_search_none"
	KeySkillsCLIFound       = "skills_cli_found"
	KeySkillsCLIEdit        = "skills_cli_edit"
	KeySkillsCLICreated     = "skills_cli_created"
	KeySkillsCLIDisabled    = "skills_cli_disabled"
	KeySkillsCLIEnabled     = "skills_cli_enabled"
	KeySkillsCLIUninstalled = "skills_cli_uninstalled"

	// MCP CLI output
	KeyMCPCLINone        = "mcp_cli_none"
	KeyMCPCLITotal       = "mcp_cli_total"
	KeyMCPCLIInstalled   = "mcp_cli_installed"
	KeyMCPCLISearchNone  = "mcp_cli_search_none"
	KeyMCPCLIFound       = "mcp_cli_found"
	KeyMCPCLIUninstalled = "mcp_cli_uninstalled"
	KeyMCPCLIEnabled     = "mcp_cli_enabled"
	KeyMCPCLIDisabled    = "mcp_cli_disabled"

	// Agent CLI output
	KeyAgentCLINone        = "agent_cli_none"
	KeyAgentCLITotal       = "agent_cli_total"
	KeyAgentCLIInstalled   = "agent_cli_installed"
	KeyAgentCLICreated     = "agent_cli_created"
	KeyAgentCLISearchNone  = "agent_cli_search_none"
	KeyAgentCLIFound       = "agent_cli_found"
	KeyAgentCLIDisabled    = "agent_cli_disabled"
	KeyAgentCLIEnabled     = "agent_cli_enabled"
	KeyAgentCLIUninstalled = "agent_cli_uninstalled"

	// Workflow CLI output
	KeyCmdWorkflowUse          = "cmd_workflow_use"
	KeyCmdWorkflowShort        = "cmd_workflow_short"
	KeyCmdWorkflowListUse      = "cmd_workflow_list_use"
	KeyCmdWorkflowListShort    = "cmd_workflow_list_short"
	KeyCmdWorkflowShowUse      = "cmd_workflow_show_use"
	KeyCmdWorkflowShowShort    = "cmd_workflow_show_short"
	KeyCmdWorkflowNewUse       = "cmd_workflow_new_use"
	KeyCmdWorkflowNewShort     = "cmd_workflow_new_short"
	KeyCmdWorkflowDeleteUse    = "cmd_workflow_delete_use"
	KeyCmdWorkflowDeleteShort  = "cmd_workflow_delete_short"
	KeyCmdWorkflowDisableUse   = "cmd_workflow_disable_use"
	KeyCmdWorkflowDisableShort = "cmd_workflow_disable_short"
	KeyCmdWorkflowEnableUse    = "cmd_workflow_enable_use"
	KeyCmdWorkflowEnableShort  = "cmd_workflow_enable_short"
	KeyWorkflowCLINone         = "workflow_cli_none"
	KeyWorkflowCLITotal        = "workflow_cli_total"
	KeyWorkflowCLICreated      = "workflow_cli_created"
	KeyWorkflowCLIDisabled     = "workflow_cli_disabled"
	KeyWorkflowCLIEnabled      = "workflow_cli_enabled"
	KeyWorkflowCLIDeleted      = "workflow_cli_deleted"
	KeyWorkflowCLIEdit         = "workflow_cli_edit"
	KeyCmdWorkflowInitUse      = "cmd_workflow_init_use"
	KeyCmdWorkflowInitShort    = "cmd_workflow_init_short"
	KeyCmdWorkflowInitComplete = "cmd_workflow_init_complete"

	// Cleanup common
	KeyCleanupComplete = "cleanup_complete"
	KeyNotExistSkip    = "not_exist_skip"
	KeyDirectory       = "directory"
)

var (
	globalLang Lang
	once       sync.Once

	// catalog stores all message translations: catalog[lang][key] = message
	catalog = map[Lang]map[string]string{}
)

func init() {
	catalog[EN] = enMessages
	catalog[ZH] = zhMessages
}

// DetectLang detects the system language from environment variables.
// Checks LANG, LC_ALL, LC_MESSAGES in order. Returns EN if undetected.
func DetectLang() Lang {
	once.Do(func() {
		globalLang = detectFromEnv()
	})
	return globalLang
}

// SetLang overrides the detected language (useful for testing or config override).
// Must be called before any TL() call to take effect, since DetectLang uses sync.Once.
func SetLang(l Lang) {
	globalLang = l
	once.Do(func() {}) // mark as done so DetectLang won't overwrite
}

// T translates a message key for the given language.
// Falls back to English if the key is not found.
func T(key string, lang Lang) string {
	if msgs, ok := catalog[lang]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	if msgs, ok := catalog[EN]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	return key
}

// TL translates a message key for the globally detected language.
func TL(key string) string {
	return T(key, DetectLang())
}

// detectFromEnv checks LANG, LC_ALL, LC_MESSAGES for locale info.
func detectFromEnv() Lang {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		val := os.Getenv(env)
		if val == "" {
			continue
		}
		val = strings.ToLower(val)
		switch {
		case strings.HasPrefix(val, "zh"):
			return ZH
		case strings.HasPrefix(val, "ja"):
			return EN // Japanese → English (no Japanese translation available)
		case strings.HasPrefix(val, "ko"):
			return EN // Korean → English (no Korean translation available)
		}
	}
	return EN
}
