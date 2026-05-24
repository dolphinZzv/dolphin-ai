// Package context builds the LLM context prompt from project configuration.
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ToolInfo holds the basic information about an MCP tool for dynamically
// generating the builtin skills documentation from the tool registry.
type ToolInfo struct {
	Name        string
	Description string
	Priority    int
}

// Default section priorities (lower = earlier in prompt).
const (
	PrioritySoul          = 1
	PriorityPreface       = 2
	PriorityBuiltinSkills = 3
	PrioritySelfEvoSkills = 4
	PriorityAgents        = 5
	PriorityRules         = 6
	PriorityUser          = 7
	PrioritySystem        = 8
	PrioritySubSystems    = 9
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

// SectionProvider provides a named section to the system prompt.
// Implementations are registered via Builder.RegisterSectionProvider.
type SectionProvider interface {
	// Name returns the section identifier (e.g. "soul", "preface", "subsystems").
	// Used for priority overrides and dedup.
	Name() string

	// Content returns the markdown content. Return "" to skip injection.
	// The agentName parameter is the target agent's name, or "" for the
	// default (coordinator) agent.
	Content(agentName string) string
}

// SectionProviderWithPath is an optional extension for SectionProviders
// that can report the file path of their content source for display.
type SectionProviderWithPath interface {
	SectionProvider

	// Path returns the resolved file path for the given agent, or "".
	Path(agentName string) string
}

// NewSectionProviderFunc creates a SectionProvider from a function.
func NewSectionProviderFunc(name string, fn func(agentName string) string) *SectionProviderFunc {
	return &SectionProviderFunc{name: name, fn: fn}
}

// SectionProviderFunc wraps a function as a SectionProvider.
type SectionProviderFunc struct {
	name string
	fn   func(agentName string) string
}

func (f *SectionProviderFunc) Name() string { return f.name }
func (f *SectionProviderFunc) Content(agentName string) string {
	if f.fn == nil {
		return ""
	}
	return f.fn(agentName)
}

// compile-time interface check.
var _ SectionProvider = (*SectionProviderFunc)(nil)

// fileProvider loads content from a file with the standard fallback chain.
type fileProvider struct {
	b             *Builder
	name          string // "soul", "agents", etc.
	filename      string // "SOUL.md", "AGENTS.md", etc.
	heading       string // "## Soul\n", "## Agent Definitions\n", etc.
	agentSpecific bool   // if true, checks agentDir first (for AGENTS.md, RULES.md, USER.md)
	userDirOnly   bool   // if true, only checks user dir (for SYSTEM.md)
}

func (p *fileProvider) Name() string { return p.name }

func (p *fileProvider) Content(agentName string) string {
	if p.userDirOnly {
		path := filepath.Join(p.b.userDir, p.filename)
		content, ok := p.b.loadCached(path)
		if !ok || content == "" {
			return ""
		}
		return p.heading + content
	}
	var agentDir string
	if p.agentSpecific && agentName != "" {
		agentDir = filepath.Join(p.b.projectDir, "agents", agentName)
	}
	content := p.b.loadFileFallback(agentDir, p.filename)
	if content == "" {
		return ""
	}
	return p.heading + content
}

func (p *fileProvider) Path(agentName string) string {
	if p.userDirOnly {
		return filepath.Join(p.b.userDir, p.filename)
	}
	var agentDir string
	if p.agentSpecific && agentName != "" {
		agentDir = filepath.Join(p.b.projectDir, "agents", agentName)
	}
	return p.b.resolveFileFallback(agentDir, p.filename)
}

// compile-time interface checks.
var (
	_ SectionProvider         = (*fileProvider)(nil)
	_ SectionProviderWithPath = (*fileProvider)(nil)
)

// registeredProvider pairs a SectionProvider with its priority and display name.
type registeredProvider struct {
	provider    SectionProvider
	priority    int
	displayName string // shown in LoadedSections output
}

// Builder assembles the system prompt from registered section providers.
type Builder struct {
	projectDir string
	userDir    string
	systemDir  string
	statCache  map[string]cachedFile
	rdata      *RenderData

	providers []registeredProvider

	// SelfEvolution controls whether SELF_EVOLUTION_SKILLS.md is included
	// in the built prompt. When enabled, LLM tools for config CRUD, skill
	// management, command management, reload, and context are documented.
	SelfEvolution bool

	// toolLister, when set, provides the list of registered MCP tools for
	// dynamic builtin skills generation. The embedded BUILTIN_SKILLS.md is
	// used as fallback when nil.
	toolLister func() []ToolInfo

	// SectionPriority overrides default section priorities.
	// Key is section provider name: "soul", "preface", "builtin_skills",
	// "self_evo_skills", "agents", "rules", "user", "system", "subsystems".
	// Value is the priority (lower = earlier in prompt).
	SectionPriority map[string]int

	// loadedSections tracks which sections were included in the last BuildForAgent call.
	loadedSections []SectionInfo
}

// SectionInfo describes a loaded section for reporting purposes.
type SectionInfo struct {
	Name     string
	Priority int
	Size     int    // content length in bytes
	Path     string // file path on disk, or "embedded" if from embedded content
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

// RegisterSectionProvider registers a section provider with the given priority
// and display name (used in LoadedSections output, e.g. "SOUL.md").
func (b *Builder) RegisterSectionProvider(provider SectionProvider, priority int, displayName string) {
	b.providers = append(b.providers, registeredProvider{
		provider:    provider,
		priority:    priority,
		displayName: displayName,
	})
}

// SectionContent returns the content of the first registered provider with the given name.
// Returns "" if no provider is found or the provider returns empty content.
func (b *Builder) SectionContent(name, agentName string) string {
	for _, rp := range b.providers {
		if rp.provider.Name() == name {
			return rp.provider.Content(agentName)
		}
	}
	return ""
}

// registerBuiltinProviders registers all built-in section providers.
func registerBuiltinProviders(b *Builder) {
	// SOUL.md (project > user > system, optional)
	b.RegisterSectionProvider(&fileProvider{
		b: b, name: "soul", filename: "SOUL.md", heading: "## Soul\n",
	}, PrioritySoul, "SOUL.md")

	// PREFACE (embedded, always)
	b.RegisterSectionProvider(NewSectionProviderFunc("preface",
		func(agentName string) string { return DefaultPreface },
	), PriorityPreface, "PREFACE.md")

	// BUILTIN SKILLS (dynamic from tool registry, falls back to embedded)
	b.RegisterSectionProvider(NewSectionProviderFunc("builtin_skills",
		func(agentName string) string {
			if b.toolLister != nil {
				return formatToolDocs(b.toolLister())
			}
			return BuiltinSkills
		},
	), PriorityBuiltinSkills, "BUILTIN_SKILLS.md")

	// SELF-EVOLUTION SKILLS (embedded, only when SelfEvolution is enabled)
	b.RegisterSectionProvider(NewSectionProviderFunc("self_evo_skills",
		func(agentName string) string {
			if !b.SelfEvolution || SelfEvolutionSkills == "" {
				return ""
			}
			return SelfEvolutionSkills
		},
	), PrioritySelfEvoSkills, "SELF_EVOLUTION.md")

	// AGENTS.md (agent > project > user > system, agent-specific)
	b.RegisterSectionProvider(&fileProvider{
		b: b, name: "agents", filename: "AGENTS.md",
		heading: "## Agent Definitions\n", agentSpecific: true,
	}, PriorityAgents, "AGENTS.md")

	// RULES.md (agent > project > user > system, agent-specific)
	b.RegisterSectionProvider(&fileProvider{
		b: b, name: "rules", filename: "RULES.md",
		heading: "## Rules\n", agentSpecific: true,
	}, PriorityRules, "RULES.md")

	// USER.md (agent > project > user > system, agent-specific)
	b.RegisterSectionProvider(&fileProvider{
		b: b, name: "user", filename: "USER.md",
		heading: "## User Context\n", agentSpecific: true,
	}, PriorityUser, "USER.md")

	// SYSTEM.md (user dir only — generated once, injected every startup)
	b.RegisterSectionProvider(&fileProvider{
		b: b, name: "system", filename: "SYSTEM.md",
		heading: "## System\n", userDirOnly: true,
	}, PrioritySystem, "SYSTEM.md")
}

func NewBuilder() *Builder {
	home, err := os.UserHomeDir()
	if err != nil {
		zap.S().Warnw("cannot determine home directory, user-level context files disabled", "error", err)
	}
	b := &Builder{
		projectDir: ".dolphin",
		userDir:    filepath.Join(home, ".dolphin"),
		systemDir:  "/etc/dolphin",
		statCache:  make(map[string]cachedFile),
	}
	registerBuiltinProviders(b)
	return b
}

// SetRenderData sets the template render data for variable injection in context files.
func (b *Builder) SetRenderData(rdata *RenderData) {
	b.rdata = rdata
}

// SetToolLister sets a function that returns the current list of registered
// MCP tools. When set, the BUILTIN_SKILLS.md section is dynamically generated
// from tool definitions (sorted by priority, ascending) instead of using the
// embedded content. Only tools that are registered (enabled) are included.
func (b *Builder) SetToolLister(fn func() []ToolInfo) {
	b.toolLister = fn
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
// Sections are provided by registered SectionProvider instances, ordered
// by priority (ascending). Default priorities can be overridden via
// Builder.SectionPriority.
//
// Additional section providers can be registered externally via
// RegisterSectionProvider (e.g. from agent/context.go for the subsystems
// provider).
func (b *Builder) BuildForAgent(agentName string) (string, error) {
	b.loadedSections = nil

	var secs []section
	for _, rp := range b.providers {
		content := rp.provider.Content(agentName)
		if content == "" {
			continue
		}
		priority := b.sectionPriority(rp.provider.Name(), rp.priority)
		secs = append(secs, section{priority: priority, content: content})

		var secPath string
		if pp, ok := rp.provider.(SectionProviderWithPath); ok {
			secPath = pp.Path(agentName)
		}
		b.loadedSections = append(b.loadedSections, SectionInfo{
			Name:     rp.displayName,
			Priority: priority,
			Size:     len(content),
			Path:     secPath,
		})
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
// If agentDir is non-empty, checks agentDir first, then falls back to
// current dir > project > user > system chain.
func (b *Builder) loadFileFallback(agentDir, name string) string {
	dirs := make([]string, 0, 5)
	if agentDir != "" {
		dirs = append(dirs, agentDir)
	}
	dirs = append(dirs, ".", b.projectDir, b.userDir, b.systemDir)

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

// resolveFileFallback returns the first existing file path matching name
// using the same fallback chain as loadFileFallback.
func (b *Builder) resolveFileFallback(agentDir, name string) string {
	dirs := make([]string, 0, 5)
	if agentDir != "" {
		dirs = append(dirs, agentDir)
	}
	dirs = append(dirs, ".", b.projectDir, b.userDir, b.systemDir)

	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
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

// formatToolDocs generates the BUILTIN_SKILLS.md content from a list of tool
// definitions, sorted by priority ascending (0 → DefaultPriority 100).
func formatToolDocs(tools []ToolInfo) string {
	if len(tools) == 0 {
		return ""
	}
	sorted := make([]ToolInfo, len(tools))
	copy(sorted, tools)
	sort.Slice(sorted, func(i, j int) bool {
		pi := sorted[i].Priority
		if pi <= 0 {
			pi = 100
		}
		pj := sorted[j].Priority
		if pj <= 0 {
			pj = 100
		}
		if pi != pj {
			return pi < pj
		}
		return sorted[i].Name < sorted[j].Name
	})

	var buf strings.Builder
	buf.WriteString("## MCP Tools Usage\n\n")
	for _, t := range sorted {
		fmt.Fprintf(&buf, "### %s\n%s\n\n", t.Name, t.Description)
	}
	return strings.TrimRight(buf.String(), "\n")
}
