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
	KeyWelcome           = "welcome"
	KeyWelcomeBanner     = "welcome_banner"
	KeySelectDomain      = "select_domain"
	KeySelectDomainHint  = "select_domain_hint"
	KeyPrivacyNote       = "privacy_note"
	KeySkip              = "skip"
	KeyChoice            = "choice"
	KeyRecommendedTools  = "recommended_tools"
	KeySkills            = "skills"
	KeyMCP               = "mcp"
	KeyInstallHint       = "install_hint"
	KeySetupHint         = "setup_hint"
	KeyNoMatch           = "no_match"
	KeySystemMDPrompt    = "system_md_prompt"
	KeySystemMDExplain   = "system_md_explain"
	KeySystemMDYes       = "system_md_yes"
	KeySystemMDNo        = "system_md_no"
	KeySystemMDSkipped   = "system_md_skipped"
	KeySystemMDGenerated = "system_md_generated"
	KeySystemMDContent   = "system_md_content"

	// Coordinator interaction
	KeyCoordReady        = "coord_ready"
	KeyHelpHeader        = "help_header"
	KeyHelpExit          = "help_exit"
	KeyHelpHelp          = "help_help"
	KeyHelpAgents        = "help_agents"
	KeyHelpSkills        = "help_skills"
	KeyHelpCommands      = "help_commands"
	KeyHelpCancel        = "help_cancel"
	KeyHelpCancelID      = "help_cancel_id"
	KeyHelpTopMCP        = "help_top_mcp"
	KeyHelpSkillsAvail   = "help_skills_avail"
	KeyNoAgents          = "no_agents"
	KeyNoAgentsHint      = "no_agents_hint"
	KeyAgentHeader       = "agent_header"
	KeySkillsNotAvail    = "skills_not_avail"
	KeyNoSkills          = "no_skills"
	KeyNoSkillsHint      = "no_skills_hint"
	KeySkillHeader       = "skill_header"
	KeySkillSearchHint   = "skill_search_hint"
	KeyCommandsNotAvail  = "commands_not_avail"
	KeyNoCommands        = "no_commands"
	KeyNoCommandsHint    = "no_commands_hint"
	KeyCommandHeader     = "command_header"
	KeyCommandRunHint    = "command_run_hint"
	KeyCronNotAvail      = "cron_not_avail"
	KeyNoCronTasks       = "no_cron_tasks"
	KeyNoCronHint        = "no_cron_hint"
	KeyCronHeader        = "cron_header"
	KeyCronRecent        = "cron_recent"
	KeyResumePrompt      = "resume_prompt"
	KeyResumeYes         = "resume_yes"
	KeyCancelAll         = "cancel_all"
	KeyCancelTask        = "cancel_task"
	KeyCancelNotFound    = "cancel_not_found"
	KeySessionCheckpoint = "session_checkpoint"
	KeyTurnError         = "turn_error"
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
			return ZH // Japanese → Chinese as closest supported
		case strings.HasPrefix(val, "ko"):
			return ZH // Korean → Chinese as closest supported
		}
	}
	return EN
}
