package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"dolphin/internal/agent/console"
	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"go.uber.org/zap"
)

// onboardConsole registers all built-in console commands.
func (c *Coordinator) onboardConsole() {
	con := console.New()

	con.Add(&console.Command{
		Name: "help", Desc: i18n.TL(i18n.KeyHelpHelp),
		Handler: func(args []string, io transport.UserIO) { c.printHelp(io) },
	})
	con.Add(&console.Command{
		Name: "sessions", Desc: i18n.TL(i18n.KeyHelpSessions),
		Children: []*console.Command{
			{Name: "dump", Desc: "Dump session events by ID", Handler: func(args []string, io transport.UserIO) { c.handleSessionDump(args, io) }},
		},
		Handler: func(args []string, io transport.UserIO) { c.handleSessions(io) },
	})
	con.Add(&console.Command{
		Name: "mcp", Desc: i18n.TL(i18n.KeyHelpMCP),
		Handler: func(args []string, io transport.UserIO) { c.printMCP(io) },
	})
	con.Add(&console.Command{
		Name: "agents", Desc: i18n.TL(i18n.KeyHelpAgents),
		Handler: func(args []string, io transport.UserIO) { c.printAgents(io) },
	})
	con.Add(&console.Command{
		Name: "crontab", Desc: i18n.TL(i18n.KeyHelpCron),
		Handler: func(args []string, io transport.UserIO) { c.printCronTasks(io) },
	})
	con.Add(&console.Command{
		Name: "skills",
		Desc: i18n.TL(i18n.KeyHelpSkills),
		Children: []*console.Command{
			{Name: "new", Desc: "Create a new skill template", Handler: func(args []string, io transport.UserIO) { c.handleSkillNew(args, io) }},
			{Name: "delete", Desc: "Delete a skill", Handler: func(args []string, io transport.UserIO) { c.handleSkillDelete(args, io) }},
			{Name: "show", Desc: "Show skill content", Handler: func(args []string, io transport.UserIO) { c.handleSkillShow(args, io) }},
		},
		Handler: func(args []string, io transport.UserIO) { c.printSkills(io) },
	})
	con.Add(&console.Command{
		Name: "commands",
		Desc: i18n.TL(i18n.KeyHelpCommands),
		Children: []*console.Command{
			{Name: "new", Desc: "Create a new command template", Handler: func(args []string, io transport.UserIO) { c.handleCmdNew(args, io) }},
			{Name: "delete", Desc: "Delete a command", Handler: func(args []string, io transport.UserIO) { c.handleCmdDelete(args, io) }},
			{Name: "show", Desc: "Show command content", Handler: func(args []string, io transport.UserIO) { c.handleCmdShow(args, io) }},
		},
		Handler: func(args []string, io transport.UserIO) { c.printCommands(io) },
	})
	con.Add(&console.Command{
		Name: "model", Desc: i18n.TL(i18n.KeyHelpModel),
		Handler: func(args []string, io transport.UserIO) { c.handleModelCmd(args, io) },
	})
	con.Add(&console.Command{
		Name: "cancel", Desc: "Cancel running tasks",
		Handler: func(args []string, io transport.UserIO) { c.handleCancelCmd(args, io) },
	})
	con.Add(&console.Command{
		Name: "context", Desc: i18n.TL(i18n.KeyHelpContext),
		Handler: func(args []string, io transport.UserIO) { c.printContext(args, io) },
	})

	c.console = con
}

func (c *Coordinator) printHelp(io transport.UserIO) {
	io.WriteLine(i18n.TL(i18n.KeyHelpHeader))
	io.WriteLine(i18n.TL(i18n.KeyHelpExit))
	io.WriteLine(i18n.TL(i18n.KeyHelpHelp))
	io.WriteLine(i18n.TL(i18n.KeyHelpAgents))
	io.WriteLine(i18n.TL(i18n.KeyHelpSkills))
	io.WriteLine(i18n.TL(i18n.KeyHelpCommands))
	io.WriteLine(i18n.TL(i18n.KeyHelpMCP))
	io.WriteLine(i18n.TL(i18n.KeyHelpStatus))
	io.WriteLine(i18n.TL(i18n.KeyHelpSessions))
	io.WriteLine(i18n.TL(i18n.KeyHelpCancel))
	io.WriteLine(i18n.TL(i18n.KeyHelpCancelID))
	io.WriteLine(i18n.TL(i18n.KeyHelpContext))
	io.WriteLine("")
	io.WriteLine(i18n.TL(i18n.KeyHelpTopMCP))
	stats := c.toolReg.ToolStats()
	toolDefs := c.toolReg.MostUsedTools(10)
	if len(toolDefs) > 0 {
		for _, t := range toolDefs {
			usage := ""
			if s, ok := stats[t.Name]; ok && s.CallCount > 0 {
				usage = fmt.Sprintf(" (%d calls)", s.CallCount)
			}
			io.WriteLine(fmt.Sprintf("  - %s: %s%s", t.Name, t.Description, usage))
		}
	}
	if c.skills != nil {
		skills := c.skills.List()
		if len(skills) > 0 {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyHelpSkillsAvail), len(skills)))
		}
	}
	io.WriteLine("")
}

func (c *Coordinator) printAgents(io transport.UserIO) {
	agents := c.pool.List()

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyAgentHeader), "AGENT", "STATUS", "TYPE", "TASKS"))
	io.WriteLine("------------------------------------------------")
	for _, a := range agents {
		io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %d",
			a.Name, a.Status, a.Kind, a.TasksDone))
	}

	// Built-in agents from registry
	if c.buildinRegistry != nil {
		for _, ba := range c.buildinRegistry.List() {
			io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %d",
				ba.Name(), "active", "buildin", 0))
		}
	}

	if len(agents) == 0 && (c.buildinRegistry == nil || len(c.buildinRegistry.List()) == 0) {
		io.WriteLine(i18n.TL(i18n.KeyNoAgents))
		io.WriteLine(i18n.TL(i18n.KeyNoAgentsHint))
	}
	io.WriteLine("")
}

func (c *Coordinator) printMCP(io transport.UserIO) {
	toolDefs := c.toolReg.List()
	if len(toolDefs) == 0 {
		io.WriteLine("No MCP tools loaded.")
		return
	}
	io.WriteLine(fmt.Sprintf("MCP tools (%d):", len(toolDefs)))
	for _, t := range toolDefs {
		src := ""
		if t.Source != "" {
			src = fmt.Sprintf(" [%s]", t.Source)
		}
		io.WriteLine(fmt.Sprintf("  %s%s - %s", t.Name, src, t.Description))
	}
	io.WriteLine("")
}

func (c *Coordinator) printSkills(io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine(i18n.TL(i18n.KeySkillsNotAvail))
		io.WriteLine(i18n.TL(i18n.KeyNoSkillsHint))
		return
	}

	skills := c.skills.List()
	if len(skills) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoSkills))
		io.WriteLine(i18n.TL(i18n.KeyNoSkillsHint))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillHeader), "SKILL", "USAGE", "DESCRIPTION"))
	io.WriteLine("----------------------------------------------------------")
	for _, s := range skills {
		usage := "0"
		if s.CallCount > 0 {
			usage = fmt.Sprintf("%d", s.CallCount)
		}
		io.WriteLine(fmt.Sprintf("%-20s %-8s %s", s.Name, usage, s.Description))
	}
	io.WriteLine("")
	io.WriteLine(i18n.TL(i18n.KeySkillSearchHint))
	io.WriteLine(i18n.TL(i18n.KeySkillDeleteUsage))
	io.WriteLine(i18n.TL(i18n.KeySkillShowUsage))
	io.WriteLine("")
}

func (c *Coordinator) handleSkillNew(args []string, io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine(i18n.TL(i18n.KeySkillsNotAvail))
		return
	}


	if len(args) == 0 {
		io.WriteLine(i18n.TL(i18n.KeySkillNewUsage))
		return
	}

	name := args[0]

	if err := c.skills.NewTemplate(name, ""); err != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillNewError), err))
		return
	}

	dir := c.skills.Dir()
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillNewCreated), name, dir))
}

func (c *Coordinator) handleSkillDelete(args []string, io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine(i18n.TL(i18n.KeySkillsNotAvail))
		return
	}

		name := ""
		if len(args) > 0 {
			name = args[0]
		}
	if name == "" {
		io.WriteLine(i18n.TL(i18n.KeySkillDeleteUsage))
		return
	}

	if _, ok := c.skills.Get(name); !ok {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillShowFail), name))
		return
	}

	if err := c.skills.Unregister(name); err != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillDeleteFail), name, err))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillDeleteDone), name))
}

func (c *Coordinator) handleSkillShow(args []string, io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine(i18n.TL(i18n.KeySkillsNotAvail))
		return
	}

		name := ""
		if len(args) > 0 {
			name = args[0]
		}
	if name == "" {
		io.WriteLine(i18n.TL(i18n.KeySkillShowUsage))
		return
	}

	s, ok := c.skills.Get(name)
	if !ok {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillShowFail), name))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillShowHeader), name))
	io.WriteLine(s.Content)
	io.WriteLine("")
}

func (c *Coordinator) printCommands(io transport.UserIO) {
	if c.commands == nil {
		io.WriteLine(i18n.TL(i18n.KeyCommandsNotAvail))
		return
	}
	cmds := c.commands.List()
	if len(cmds) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoCommands))
		io.WriteLine(i18n.TL(i18n.KeyNoCommandsHint))
		return
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCommandHeader), "COMMAND", "DESCRIPTION"))
	io.WriteLine("------------------------------------------")
	for _, cmd := range cmds {
		io.WriteLine(fmt.Sprintf("/%-19s  %s", cmd.Name, cmd.Description))
	}
	io.WriteLine("")
	io.WriteLine(i18n.TL(i18n.KeyCommandRunHint))
	io.WriteLine(i18n.TL(i18n.KeyCmdNewUsage))
	io.WriteLine(i18n.TL(i18n.KeyCmdDeleteUsage))
	io.WriteLine(i18n.TL(i18n.KeyCmdShowUsage))
	io.WriteLine("")
}

func (c *Coordinator) handleCmdNew(args []string, io transport.UserIO) {
	if c.commands == nil {
		io.WriteLine(i18n.TL(i18n.KeyCommandsNotAvail))
		return
	}

	if len(args) == 0 {

		io.WriteLine(i18n.TL(i18n.KeyCmdNewUsage))
		return
	}

	name := args[0]
	description := name
	if len(args) > 1 {
		description = strings.Join(args[1:], " ")
	}

	if err := c.commands.NewTemplate(name, description); err != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdNewError), err))
		return
	}

	dir := c.commands.Dir()
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdNewCreated), name, dir))
}

func (c *Coordinator) handleCmdDelete(args []string, io transport.UserIO) {
	if c.commands == nil {
		io.WriteLine(i18n.TL(i18n.KeyCommandsNotAvail))
		return
	}

			name := ""
		if len(args) > 0 {
			name = args[0]
		}
	if name == "" {
		io.WriteLine(i18n.TL(i18n.KeyCmdDeleteUsage))
		return
	}

	if _, ok := c.commands.Get(name); !ok {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdShowFail), name))
		return
	}

	if err := c.commands.Unregister(name); err != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdDeleteFail), name, err))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdDeleteDone), name))
}

func (c *Coordinator) handleCmdShow(args []string, io transport.UserIO) {
	if c.commands == nil {
		io.WriteLine(i18n.TL(i18n.KeyCommandsNotAvail))
		return
	}

			name := ""
		if len(args) > 0 {
			name = args[0]
		}
	if name == "" {
		io.WriteLine(i18n.TL(i18n.KeyCmdShowUsage))
		return
	}

	cmd, ok := c.commands.Get(name)
	if !ok {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdShowFail), name))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCmdShowHeader), name))
	io.WriteLine(cmd.Content)
	io.WriteLine("")
}

func (c *Coordinator) printCronTasks(io transport.UserIO) {
	if c.cronMgr == nil {
		io.WriteLine(i18n.TL(i18n.KeyCronNotAvail))
		return
	}
	tasks := c.cronMgr.List()
	if len(tasks) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoCronTasks))
		io.WriteLine(i18n.TL(i18n.KeyNoCronHint))
		return
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCronHeader), "NAME", "SCHEDULE", "STATUS"))
	io.WriteLine("-----------------------------------------------------")
	for _, t := range tasks {
		status := "enabled"
		if !t.Enabled {
			status = "disabled"
		}
		io.WriteLine(fmt.Sprintf("%-20s %-12s %s", t.Name, t.Schedule, status))
	}
	results := c.cronMgr.PendingResults()
	if len(results) > 0 {
		io.WriteLine("")
		io.WriteLine(i18n.TL(i18n.KeyCronRecent))
		for _, r := range results {
			mark := "✓"
			if !r.Success {
				mark = "✗"
			}
			msg := r.Output
			if r.Error != "" {
				msg = r.Error
			}
			if len(msg) > 100 {
				msg = msg[:100] + "..."
			}
			io.WriteLine(fmt.Sprintf("  %s %s (%s): %s", mark, r.TaskName, r.CompletedAt.Format("15:04"), msg))
		}
	}
	io.WriteLine("")
}

func (c *Coordinator) handleModelCmd(args []string, io transport.UserIO) {
	providers := c.availableProviders
	if len(providers) == 0 {
		io.WriteLine("No providers configured")
		return
	}

	if len(args) == 0 {
		// List providers
		io.WriteLine("Available providers (type:model):")
		io.WriteLine("  " + fmt.Sprintf("%-20s %-30s %s", "NAME", "MODEL", "STATUS"))
		io.WriteLine("  " + strings.Repeat("-", 55))
		for i, pc := range providers {
			status := ""
			if i == c.providerIndex {
				status = "← active"
			}
			io.WriteLine("  " + fmt.Sprintf("%-20s %-30s %s", pc.Name, pc.Model, status))
		}
		io.WriteLine("")
		io.WriteLine("Use /model <name> to switch")
		return
	}

	// Switch to named provider
	name := args[0]
	if c.switchToProvider(name) {
		io.WriteLine(fmt.Sprintf("Switched to %s (%s)", name, c.provider.Name()))
	} else {
		io.WriteLine(fmt.Sprintf("provider.Provider %q not found or unhealthy", name))
	}
}

func (c *Coordinator) handleCancelCmd(args []string, io transport.UserIO) {
	if len(args) > 0 {
		taskID := args[0]
		if c.pool.Cancel(taskID) {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCancelTask), taskID))
		} else {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCancelNotFound), taskID))
		}
	} else {
		c.pool.CancelAll()
		io.WriteLine(i18n.TL(i18n.KeyCancelAll))
	}
}

func (c *Coordinator) handleStatus(sess *session.Session, state *LoopState, io transport.UserIO) {
	io.WriteLine(i18n.TL(i18n.KeyStatusHeader))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusProvider), c.provider.Name()))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusModel), c.cfg.LLM.Model))

	if sess != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusSession), sess.ID, state.Turn))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyNoSession))
	}

	agents := c.pool.List()
	busy := 0
	for _, a := range agents {
		if a.Status == "busy" {
			busy++
		}
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusAgents), len(agents), busy))

	tools := c.toolReg.List()
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusMCPTools), len(tools)))

	if c.skills != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusSkills), len(c.skills.List())))
	}
	if c.commands != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusCommands), len(c.commands.List())))
	}
	if c.cronMgr != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusCron), len(c.cronMgr.List())))
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusMemory), m.Alloc/1024/1024))
	io.WriteLine("")
}

func (c *Coordinator) printContext(args []string, io transport.UserIO) {
	// If a section name is given, display its content
	if len(args) > 0 {
		sectionName := args[0]
		if !strings.HasSuffix(sectionName, ".md") {
			sectionName += ".md"
		}
		content := c.ctxBuilder.LoadSection(sectionName)
		if content == "" {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSectionNF), sectionName))
			return
		}
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSectionHd)+"\n", strings.ToUpper(sectionName)))
		io.WriteLine(content)
		return
	}

	io.WriteLine(i18n.TL(i18n.KeyContextSummaryHd) + "\n")

	sessionID := "none"
	if c.currentSess != nil {
		sessionID = string(c.currentSess.ID)
	}
	io.WriteLine(fmt.Sprintf("Session:      %s", sessionID))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextProvider), c.cfg.LLM.Model, c.provider.Name()))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextConfigPath), len(configurablePaths)))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextMCPTools), len(c.toolReg.List())))

	agents := c.pool.List()
	busyCount := 0
	for _, a := range agents {
		if a.Status == "busy" {
			busyCount++
		}
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextAgents), len(agents), busyCount))

	if c.skills != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSkills), len(c.skills.List())))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyContextSkillsNA))
	}
	if c.commands != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextCommands), len(c.commands.List())))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyContextCommandsNA))
	}
	if c.cronMgr != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextCron), len(c.cronMgr.List())))
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	io.WriteLine(fmt.Sprintf("Memory:       %d MB used", m.Alloc/1024/1024))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSelfEvolve), c.cfg.Flags.SelfEvolution))
	io.WriteLine("")
	if loaded := c.ctxBuilder.LoadedSections(); len(loaded) > 0 {
		io.WriteLine(i18n.TL(i18n.KeyContextSectionsHd))
		for _, s := range loaded {
			io.WriteLine(fmt.Sprintf("  %-20s %d", s.Name, s.Priority))
		}
		io.WriteLine("")
	}
}

func (c *Coordinator) handleSessions(io transport.UserIO) {
	dir := c.sessMgr.Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Cannot read sessions dir: %v", err))
		return
	}

	type sessInfo struct {
		id    string
		turns int
		mod   time.Time
	}
	var sessions []sessInfo
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, "-summary.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		turns, _ := session.CountTurns(filepath.Join(dir, name))
		sessions = append(sessions, sessInfo{id: id, turns: turns, mod: info.ModTime()})
	}

	if len(sessions) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoSessions))
		io.WriteLine("")
		return
	}

	// Sort by modification time, most recent first
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].mod.After(sessions[j].mod)
	})

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySessionsHeader), len(sessions)))
	for _, s := range sessions {
		shortID := s.id
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		ago := time.Since(s.mod).Truncate(time.Second).String()
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySessionRow), shortID, s.turns, ago+" ago"))
	}
	io.WriteLine("")
}

func (c *Coordinator) handleSessionDump(args []string, io transport.UserIO) {
	if len(args) == 0 {
		io.WriteLine("Usage: /sessions dump <session-id>")
		return
	}
	sessionPath := filepath.Join(c.sessMgr.Dir(), args[0]+".jsonl")
	events, err := session.ReadEvents(sessionPath)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Failed to read session: %v", err))
		return
	}
	io.WriteLine(fmt.Sprintf("Session: %s (%d events)\n", args[0], len(events)))
	for _, evt := range events {
		line := fmt.Sprintf("[T%d %s] %s", evt.Turn, evt.Timestamp.Format("15:04:05"), evt.Type)
		if evt.Role != "" {
			line += fmt.Sprintf(" (%s)", evt.Role)
		}
		if evt.ToolName != "" {
			line += fmt.Sprintf(" tool=%s", evt.ToolName)
		}
		if len(evt.Content) > 0 {
			content := string(evt.Content)
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			line += ": " + content
		}
		io.WriteLine(line)
	}
	io.WriteLine("")
}

func (c *Coordinator) handleNew(sess *session.Session, state *LoopState, io transport.UserIO) {
	oldTurns := state.Turn
	oldID := sess.ID

	// Generate summary and close the old session
	c.generateSummary(sess, state)
	sess.Close()
	c.sessMgr.Remove(sess.ID)

	// Create a new child session
	newSess, err := c.sessMgr.NewSession(c.cfg.Session.MaxLoop)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Failed to create new session: %v", err))
		return
	}
	newSess.LogSystem(fmt.Sprintf("fresh start from session %s (turn %d)", oldID, oldTurns))

	// Reset state with new session, preserving config and transport
	state.Sess = newSess
	state.Turn = 0
	state.Messages = nil
	state.ToolCallCount = 0
	state.ErrorCount = 0
	state.StopReason = ""
	state.SummaryGenerated = false
	c.currentSess = newSess

	zap.S().Infow("session reset via /new",
		"old_session", oldID,
		"new_session", newSess.ID,
	)

	io.WriteLine(fmt.Sprintf("New session %s started. Previous: %s (%d turns)",
		newSess.ID, oldID, oldTurns,
	))
	io.WriteLine("")
}
