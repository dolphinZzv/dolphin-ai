package context

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

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

// Build builds the system prompt for the default (coordinator) agent.
func (b *Builder) Build() (string, error) {
	return b.BuildForAgent("")
}

// BuildForAgent builds a system prompt for a specific agent.
// For each context file, agent-specific directory is checked first, then
// the default locations: project > user > system.
//
//	agentDir = .dolphin/agents/<name>/
//	order for AGENTS.md: agentDir > projectDir > userDir > systemDir
//	order for RULES.md:  agentDir > projectDir > userDir > systemDir
//	order for USER.md:   agentDir > projectDir > userDir > systemDir
//	SYSTEM.md: user dir only — generated once, injected every session
func (b *Builder) BuildForAgent(agentName string) (string, error) {
	var agentDir string
	if agentName != "" {
		agentDir = filepath.Join(b.projectDir, "agents", agentName)
	}

	var parts []string

	// 1. PREFACE (embedded, always)
	parts = append(parts, DefaultPreface)

	// 2. AGENTS.md (agent > project > user > system)
	if agents := b.loadFileFallback(agentDir, "AGENTS.md"); agents != "" {
		parts = append(parts, "## Agent Definitions\n"+agents)
	}

	// 3. RULES.md
	if rules := b.loadFileFallback(agentDir, "RULES.md"); rules != "" {
		parts = append(parts, "## Rules\n"+rules)
	}

	// 4. USER.md
	if user := b.loadFileFallback(agentDir, "USER.md"); user != "" {
		parts = append(parts, "## User Context\n"+user)
	}

	// 5. SYSTEM.md (user dir only — generated once, injected every startup)
	if sys := b.loadSystemMD(); sys != "" {
		parts = append(parts, "## System\n"+sys)
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

	b.statCache[path] = cachedFile{
		content: string(data),
		modTime: info.ModTime(),
	}
	return b.statCache[path].content, true
}
