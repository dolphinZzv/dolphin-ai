// Package context builds the LLM context prompt from project configuration.
package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Default section priorities (lower = earlier in prompt).
const (
	PrioritySoul              = 1
	PriorityPreface           = 2
	PriorityBuiltinSkills     = 3
	PrioritySelfEvoSkills     = 4
	PriorityAgents            = 5
	PriorityRules             = 6
	PriorityUser              = 7
	PrioritySystem            = 8
)

// section holds a single prompt section with its priority.
type section struct {
	priority int
	content  string
}

// cachedFile holds the cached content and modification time of a context file.
type cachedFile struct {
	content string
	modTime time.Time
}

// Builder assembles the system prompt from context files.
type Builder struct {
	projectDir string
	userDir    string
	systemDir  string
	statCache  map[string]cachedFile
	rdata      *RenderData

	// SelfEvolution controls whether SELF_EVOLUTION_SKILLS.md is included
	// in the built prompt. When enabled, LLM tools for config CRUD, skill
	// management, command management, reload, and context are documented.
	SelfEvolution bool

	// SectionPriority overrides default section priorities.
	// Key is section name: "soul", "preface", "builtin_skills",
	// "self_evo_skills", "agents", "rules", "user", "system".
	// Value is the priority (lower = earlier in prompt).
	SectionPriority map[string]int

	// loadedSections tracks which sections were included in the last BuildForAgent call.
	loadedSections []SectionInfo
}

// SectionInfo describes a loaded section for reporting purposes.
type SectionInfo struct {
	Name     string
	Priority int
	Size     int // content length in bytes
}

// LoadedSections returns info about sections included in the last build.
func (b *Builder) LoadedSections() []SectionInfo {
	return b.loadedSections
}

// LoadSection loads a single context section by filename (e.g. "SYSTEM.md").
// Checks embedded content first (PREFACE.md, BUILTIN_SKILLS.md, SELF_EVOLUTION_SKILLS.md),
// then falls back to the standard project > user > system chain.
func (b *Builder) LoadSection(name string) string {
	switch name {
	case "PREFACE.md":
		return DefaultPreface
	case "BUILTIN_SKILLS.md":
		return BuiltinSkills
	case "SELF_EVOLUTION_SKILLS.md", "SELF_EVOLUTION.md":
		return SelfEvolutionSkills
	default:
		return b.loadFileFallback("", name)
	}
}

func NewBuilder() *Builder {
	home, err := os.UserHomeDir()
	if err != nil {
		zap.S().Warnw("cannot determine home directory, user-level context files disabled", "error", err)
	}
	return &Builder{
		projectDir: ".dolphin",
		userDir:    filepath.Join(home, ".dolphin"),
		systemDir:  "/etc/dolphin",
		statCache:  make(map[string]cachedFile),
	}
}

// SetRenderData sets the template render data for variable injection in context files.
func (b *Builder) SetRenderData(rdata *RenderData) {
	b.rdata = rdata
}

// Build builds the system prompt for the default (coordinator) agent.
func (b *Builder) Build() (string, error) {
	return b.BuildForAgent("")
}

// sectionPriority returns the effective priority for a named section,
// using the user override if set, otherwise the default.
func (b *Builder) sectionPriority(name string, defaultPriority int) int {
	if b.SectionPriority != nil {
		if p, ok := b.SectionPriority[name]; ok {
			return p
		}
	}
	return defaultPriority
}

// BuildForAgent builds a system prompt for a specific agent.
// For each context file, agent-specific directory is checked first, then
// the default locations: project > user > system.
//
//	agentDir = .dolphin/agents/<name>/
//	SOUL.md:	projectDir > userDir > systemDir (optional)
//	order for AGENTS.md: agentDir > projectDir > userDir > systemDir
//	order for RULES.md:  agentDir > projectDir > userDir > systemDir
//	order for USER.md:   agentDir > projectDir > userDir > systemDir
//	SYSTEM.md: user dir only — generated once, injected every startup
//
// Sections are ordered by priority (ascending). Default priorities can be
// overridden via Builder.SectionPriority.
func (b *Builder) BuildForAgent(agentName string) (string, error) {
	var agentDir string
	if agentName != "" {
		agentDir = filepath.Join(b.projectDir, "agents", agentName)
	}

	var secs []section
	b.loadedSections = nil

	// SOUL.md (project > user > system, optional)
	if soul := b.loadFileFallback("", "SOUL.md"); soul != "" {
		content := "## Soul\n" + soul
		secs = append(secs, section{
			priority: b.sectionPriority("soul", PrioritySoul),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "SOUL.md", Priority: b.sectionPriority("soul", PrioritySoul), Size: len(content)})
	}

	// PREFACE (embedded, always)
	content := DefaultPreface
	secs = append(secs, section{
		priority: b.sectionPriority("preface", PriorityPreface),
		content:  content,
	})
	b.loadedSections = append(b.loadedSections, SectionInfo{Name: "PREFACE.md", Priority: b.sectionPriority("preface", PriorityPreface), Size: len(content)})

	// BUILTIN SKILLS (embedded, always)
	if BuiltinSkills != "" {
		content := BuiltinSkills
		secs = append(secs, section{
			priority: b.sectionPriority("builtin_skills", PriorityBuiltinSkills),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "BUILTIN_SKILLS.md", Priority: b.sectionPriority("builtin_skills", PriorityBuiltinSkills), Size: len(content)})
	}

	// SELF-EVOLUTION SKILLS (embedded, only when SelfEvolution is enabled)
	if b.SelfEvolution && SelfEvolutionSkills != "" {
		content := SelfEvolutionSkills
		secs = append(secs, section{
			priority: b.sectionPriority("self_evo_skills", PrioritySelfEvoSkills),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "SELF_EVOLUTION.md", Priority: b.sectionPriority("self_evo_skills", PrioritySelfEvoSkills), Size: len(content)})
	}

	// AGENTS.md (agent > project > user > system)
	if agents := b.loadFileFallback(agentDir, "AGENTS.md"); agents != "" {
		content := "## Agent Definitions\n" + agents
		secs = append(secs, section{
			priority: b.sectionPriority("agents", PriorityAgents),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "AGENTS.md", Priority: b.sectionPriority("agents", PriorityAgents), Size: len(content)})
	}

	// RULES.md
	if rules := b.loadFileFallback(agentDir, "RULES.md"); rules != "" {
		content := "## Rules\n" + rules
		secs = append(secs, section{
			priority: b.sectionPriority("rules", PriorityRules),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "RULES.md", Priority: b.sectionPriority("rules", PriorityRules), Size: len(content)})
	}

	// USER.md
	if user := b.loadFileFallback(agentDir, "USER.md"); user != "" {
		content := "## User Context\n" + user
		secs = append(secs, section{
			priority: b.sectionPriority("user", PriorityUser),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "USER.md", Priority: b.sectionPriority("user", PriorityUser), Size: len(content)})
	}

	// SYSTEM.md (user dir only — generated once, injected every startup)
	if sys := b.loadSystemMD(); sys != "" {
		content := "## System\n" + sys
		secs = append(secs, section{
			priority: b.sectionPriority("system", PrioritySystem),
			content:  content,
		})
		b.loadedSections = append(b.loadedSections, SectionInfo{Name: "SYSTEM.md", Priority: b.sectionPriority("system", PrioritySystem), Size: len(content)})
	}

	// Sort by priority ascending
	sort.Slice(secs, func(i, j int) bool {
		return secs[i].priority < secs[j].priority
	})

	parts := make([]string, len(secs))
	for i, s := range secs {
		parts[i] = s.content
	}
	return strings.Join(parts, "\n\n"), nil
}

// loadSystemMD loads SYSTEM.md from the user config directory only.
// This file is generated once on first startup, injected into every session.
func (b *Builder) loadSystemMD() string {
	path := filepath.Join(b.userDir, "SYSTEM.md")
	content, ok := b.loadCached(path)
	if !ok {
		return ""
	}
	zap.S().Infow("loaded SYSTEM.md", "path", path)
	return content
}

// loadFileFallback loads a context file with cascading fallback.
// If agentDir is non-empty, checks agentDir first, then falls back to the
// default project > user > system chain.
func (b *Builder) loadFileFallback(agentDir, name string) string {
	dirs := make([]string, 0, 4)
	if agentDir != "" {
		dirs = append(dirs, agentDir)
	}
	dirs = append(dirs, b.projectDir, b.userDir, b.systemDir)

	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		content, ok := b.loadCached(path)
		if !ok {
			continue
		}
		if content != "" {
			zap.S().Debugw("loaded context file", "file", path)
			return content
		}
	}
	return ""
}

// loadCached reads a file with stat-based caching. Returns (content, true) on
// success, or ("", false) if the file doesn't exist. Permission or IO errors
// are logged at Warn level and also return ("", false).
// Template expansion via text/template is applied when render data is set.
func (b *Builder) loadCached(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		zap.S().Warnw("cannot stat context file", "path", path, "error", err)
		return "", false
	}

	if cached, ok := b.statCache[path]; ok && cached.modTime.Equal(info.ModTime()) {
		return cached.content, true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		zap.S().Warnw("cannot read context file", "path", path, "error", err)
		return "", false
	}

	content := string(data)
	if b.rdata != nil {
		content = expandTemplate(path, content, b.rdata)
	}

	b.statCache[path] = cachedFile{
		content: content,
		modTime: info.ModTime(),
	}
	return b.statCache[path].content, true
}
