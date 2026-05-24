package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"dolphin/internal/agent/buildin"
	"dolphin/internal/agent/compressor"
	"dolphin/internal/agent/console"
	"dolphin/internal/agent/provider"
	"dolphin/internal/command"
	ctxpkg "dolphin/internal/context"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/i18n"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/subsystem"
	"dolphin/internal/transport"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Coordinator wraps an Agent with multi-agent coordination capabilities.
type Coordinator struct {
	agent            *Agent
	pool             *AgentPool
	skills           *skill.Manager
	commands         *command.Manager
	cronMgr          *scheduler.Manager
	console          *console.Console
	basePrompt       string
	pending          []TaskResult    // results collected but not yet in LLM context
	loadedTools      map[string]bool // MCP tools loaded by LLM via load_mcp_tools
	buildinRegistry  *buildin.BuildinRegistry
	buildinCancelFns map[string][]func() // per-agent unsubscribe funcs
	currentSess      *session.Session    // current session for buildin logging
	reloadRequested  bool                // set by reload tool to trigger clean exit

	// cumulative token usage across the entire session (for /context display)
	totalInputTokens  int
	totalOutputTokens int
	totalCachedTokens int
	totalMissedTokens int

	// last LLM request content (for /context current display)
	lastLLMSystemPrompt string
	lastLLMMessages     []provider.Message
}

// NewCoordinator creates a coordinator from an existing Agent and agent pool.
// The tool registry is cloned so that coordinator tool registration (dispatch_task,
// create_agent, etc.) does not overwrite handlers on the shared registry across
// multiple transport connections.
func NewCoordinator(agent *Agent, pool *AgentPool) *Coordinator {
	// Clone the tool registry for per-coordinator tool registration
	coordReg := agent.toolReg.Clone()
	comp := agent.compressor
	if comp == nil {
		comp = &compressor.DropCompressor{}
	}
	coordAgent := &Agent{
		cfg:                agent.cfg.Clone(),
		sessMgr:            agent.sessMgr,
		toolReg:            coordReg,
		provider:           agent.provider,
		ctxBuilder:         agent.ctxBuilder,
		compressor:         comp,
		hooks:              agent.hooks,
		events:             agent.events,
		version:            agent.version,
		buildTime:          agent.buildTime,
		commitHash:         agent.commitHash,
		availableProviders: agent.availableProviders,
		providerIndex:      agent.providerIndex,
	}
	// Core coordinator tools always available; MCP tools loaded on demand.
	coreTools := []string{"dispatch_task", "create_agent", "get_agent_status",
		"cancel_task", "delete_agent", "search_skills", "load_skill",
		"add_cron_task", "remove_cron_task", "list_cron_tasks", "toggle_cron_task",
		"config", "load_mcp_tools", "search_mcp_tools"}
	if agent.cfg.Flags.SelfEvolution {
		coreTools = append(coreTools,
			"create_skill", "update_skill", "delete_skill",
			"create_command", "update_command", "delete_command",
			"reload", "context")
	}
	loaded := make(map[string]bool, len(coreTools))
	for _, name := range coreTools {
		loaded[name] = true
	}
	coord := &Coordinator{
		agent:            coordAgent,
		pool:             pool,
		loadedTools:      loaded,
		buildinRegistry:  buildin.GetRegistry(),
		buildinCancelFns: make(map[string][]func()),
	}
	coord.onboardConsole()
	return coord
}

// SetSkillManager sets the skill manager for skills support.
// Should be called before Run().
func (c *Coordinator) SetSkillManager(mgr *skill.Manager) {
	c.skills = mgr
}

// SetCommandManager sets the command manager for user-defined /commands.
func (c *Coordinator) SetCommandManager(mgr *command.Manager) {
	c.commands = mgr
}

// SetCronManager sets the cron task manager for scheduled tasks.
func (c *Coordinator) SetCronManager(mgr *scheduler.Manager) {
	c.cronMgr = mgr
}

// initBuildinAgents initializes all registered built-in agents. It wires
// dispatch, session logging, and telemetry span creation into each agent's
// handle, then calls Init() on each agent so they can subscribe to events.
func (c *Coordinator) initBuildinAgents(ctx context.Context) {
	if c.agent.events == nil || c.buildinRegistry == nil || len(c.buildinRegistry.List()) == 0 {
		return
	}

	// Wire up dispatch — use RunTask directly (buildin agents are not pool agents)
	dispatchTask := func(ctx context.Context, agentName, prompt string) (string, error) {
		taskID := xid.New().String()
		parentID := session.SessionID("")
		if c.currentSess != nil {
			parentID = c.currentSess.ID
		}
		result, err := c.agent.RunTask(ctx, prompt, c.basePrompt, c.agent.toolReg, parentID)
		if err != nil {
			return taskID, err
		}
		if !result.Success {
			return taskID, fmt.Errorf("%s", result.Error)
		}
		return taskID, nil
	}

	// Wire up session logging (no-op if no session)
	logEvent := func(ctx context.Context, evtType string, data map[string]any) {
		if c.currentSess == nil {
			return
		}
		content, _ := json.Marshal(data)
		_ = c.currentSess.LogEvent(session.SessionEvent{
			Type:    session.EventAgentAction,
			Content: content,
		})
	}

	// Wire up OTel span creation
	startSpan := func(ctx context.Context, agentName, triggerEvent string) func() {
		if TelemetryCallbacks.OnBuildinSpan != nil {
			return TelemetryCallbacks.OnBuildinSpan(ctx, agentName, triggerEvent)
		}
		return func() {}
	}

	handle := buildin.NewAgentHandle(c.agent.events, dispatchTask, logEvent, startSpan)

	for _, ba := range c.buildinRegistry.List() {
		ba.Init(ctx, handle)
		zap.S().Infow("buildin agent initialized", "agent", ba.Name())
	}

	// Emit app:started once across all transports
	appStartedOnce.Do(func() {
		c.agent.events.Emit(ctx, event.Event{Type: event.TypeAppStarted})
		zap.S().Infow("buildin agents ready")
	})
}

var (
	// appStartedOnce ensures app:started fires only once per process lifetime.
	appStartedOnce sync.Once
	// appStoppedOnce ensures app:stopped fires only once per process lifetime.
	appStoppedOnce sync.Once
)

// Run starts the coordinator event loop.
func (c *Coordinator) Run(ctx context.Context, io transport.UserIO) {
	zap.S().Infow("coordinator starting")

	// Wrap non-streaming transports with BufferedIO so all writes are
	// buffered until Flush() is called (auto-flushed on ReadLine).
	if !io.Capabilities().Streaming {
		io = transport.NewBufferedIO(io)
	}
	defer func() {
		if err := io.Flush(); err != nil {
			zap.S().Warnw("flush failed on coordinator exit", "error", err)
		}
	}()

	// Create or resume session
	var err error
	sess, state := c.tryResumeSession(ctx, io)
	if sess == nil {
		sess, err = c.agent.sessMgr.NewSession(c.agent.cfg.Session.MaxLoop)
		if err != nil {
			zap.S().Errorw("create session failed", "error", err)
			return
		}
		state = &LoopState{Sess: sess}
	}
	c.currentSess = sess

	defer func() {
		// Fire transport:disconnect hook
		if c.agent.hooks != nil {
			c.agent.hooks.Fire(ctx, hook.PointTransportDisconnect, &hook.Context{
				SessionID:     string(sess.ID),
				TransportName: io.Name(),
				Turn:          state.Turn,
			})
		}
		// Fire session:end hook
		if c.agent.hooks != nil {
			c.agent.hooks.Fire(ctx, hook.PointSessionEnd, &hook.Context{
				SessionID: string(sess.ID),
				Turn:      state.Turn,
			})
		}
		c.agent.generateSummary(sess, state)
		sess.Close()
		c.agent.sessMgr.Remove(sess.ID)
		c.pool.Shutdown()
	}()

	// Build base system prompt
	c.agent.ctxBuilder.SetRenderData(ctxpkg.NewRenderData(c.agent.cfg))
	c.agent.ctxBuilder.SelfEvolution = c.agent.cfg.Flags.SelfEvolution
	c.basePrompt, err = c.agent.ctxBuilder.Build()
	if err != nil {
		zap.S().Errorw("build context failed", "error", err)
		return
	}

	// Inject transport-specific context
	if tc := io.Context(); tc != "" {
		c.basePrompt += "\n\n## Transport\n" + tc
	}

	// Register coordinator tools on the agent's tool registry
	c.registerCoordinatorTools()

	// Auto-load enabled MCP tools so the LLM doesn't waste turns discovering them
	c.autoLoadMCPTools()

	// Link pool to coordinator session for sub-agent session tracing
	c.pool.SetParentSessionID(sess.ID)

	// Fire session:start hook
	if c.agent.hooks != nil {
		c.agent.hooks.Fire(ctx, hook.PointSessionStart, &hook.Context{
			SessionID: string(sess.ID),
			Turn:      0,
			Values:    make(map[string]any),
		})
	}

	// Fire transport:connect hook
	if c.agent.hooks != nil {
		c.agent.hooks.Fire(ctx, hook.PointTransportConnect, &hook.Context{
			SessionID:     string(sess.ID),
			TransportName: io.Name(),
		})
	}

	zap.S().Debugw("coordinator session started",
		"session_id", sess.ID,
		"max_loop", c.agent.cfg.Session.MaxLoop,
		"model", c.agent.cfg.LLM.Model,
	)

	// Start cron task processor
	if c.cronMgr != nil {
		dueCh := c.cronMgr.Start(ctx)
		go c.processDueTasks(ctx, dueCh, sess.ID)
	}

	// Initialize built-in agents (event subscriptions + session/OTel recording)
	c.initBuildinAgents(ctx)

	io.WriteLine(fmt.Sprintf("dolphin %s (%s/%s) %s — Coordinator Ready", c.agent.version, runtime.GOOS, runtime.Version(), c.agent.commitHash))
	io.WriteLine(i18n.TL(i18n.KeyCoordReady))

	for {
		select {
		case <-ctx.Done():
			state.StopReason = "interrupted"
			appStoppedOnce.Do(func() {
				if c.agent.events != nil {
					c.agent.events.Emit(ctx, event.Event{Type: event.TypeAppStopped})
				}
			})
			return
		default:
		}

		// Check for reload request before next turn
		if c.reloadRequested {
			state.StopReason = "reload"
			return
		}

		// Check max loop
		if state.Turn >= c.agent.cfg.Session.MaxLoop && !state.SummaryGenerated {
			state.SummaryGenerated = true
			zap.S().Infow("max loop reached, generating summary", "turns", state.Turn)
			c.agent.generateSummary(sess, state)
			io.WriteLine(i18n.TL(i18n.KeySessionCheckpoint))
		}

		line, err := io.ReadLine()
		if err != nil {
			zap.S().Debugw("read line error", "error", err)
			state.StopReason = "transport_error"
			return
		}

		// Fire transport:receive hook
		if c.agent.hooks != nil {
			c.agent.hooks.Fire(ctx, hook.PointTransportReceive, &hook.Context{
				SessionID:     string(sess.ID),
				Turn:          state.Turn + 1,
				UserInput:     line,
				TransportName: io.Name(),
			})
		}

		// Fire user:input hook (can rewrite or reject input)
		if c.agent.hooks != nil {
			hc := &hook.Context{
				SessionID: string(sess.ID),
				Turn:      state.Turn + 1,
				UserInput: line,
				Values:    make(map[string]any),
			}
			if err := c.agent.hooks.Fire(ctx, hook.PointUserInput, hc); err != nil {
				io.WriteLine(fmt.Sprintf("[Rejected: %v]", err))
				continue
			}
			line = hc.UserInput
		}

		// Handle commands
		switch {
		case line == "/exit" || line == "exit" || line == "quit":
			state.StopReason = "user_exit"
			return
		case line == "/new":
			c.handleNew(sess, state, io)
			sess = state.Sess
			continue
		case line == "/status":
			c.handleStatus(sess, state, io)
			continue
		case c.console.Execute(line, io):
			continue
		case line == "":
			continue
		}

		// User-defined /commands (fallthrough after console)
		var matchedUserCmd bool
		if c.commands != nil && strings.HasPrefix(line, "/") {
			if cmdName := parseCommandName(line); cmdName != "" {
				if cmd, ok := c.commands.Get(cmdName); ok {
					c.commands.RecordUsage(cmdName)
					var sb strings.Builder
					sb.WriteString("User triggered command /")
					sb.WriteString(cmdName)
					sb.WriteString("\n\n")
					sb.WriteString(cmd.Content)
					rest := strings.TrimSpace(line[len("/"+cmdName):])
					if rest != "" {
						sb.WriteString("\n\nUser arguments: ")
						sb.WriteString(rest)
					}
					line = sb.String()
					matchedUserCmd = true
				}
			}
		}
		if matchedUserCmd {
			// Will fall through to LLM processing below
		} else if strings.HasPrefix(line, "/") {
			line = strings.TrimPrefix(line, "/")
		}

		state.Turn++
		sess.Turn = state.Turn

		// Collect pending agent results
		collected := c.pool.Collect()
		c.pending = append(c.pending, collected...)
		// Emit agent:completed events
		if c.agent.events != nil {
			for _, r := range collected {
				c.agent.events.Emit(ctx, event.Event{
					Type:      event.TypeAgentCompleted,
					SessionID: string(sess.ID),
					Data: map[string]any{
						"agent_name":  r.AgentName,
						"task_id":     r.TaskID,
						"success":     r.Success,
						"duration_ms": r.DurationMs,
					},
				})
			}
		}

		// Bound pending slice to prevent unbounded growth (P0#1)
		maxResults := c.agent.cfg.Pool.MaxPendingResults
		if maxResults <= 0 {
			maxResults = 10
		}
		if len(c.pending) > maxResults*2 {
			c.pending = c.pending[len(c.pending)-maxResults:]
		}

		// Build dynamic system prompt with current context
		dynamicPrompt := c.buildDynamicPrompt()

		// Add user message
		userContent := provider.TextContent(line)
		state.Messages = append(state.Messages, provider.Message{Role: "user", Content: userContent})
		sess.LogMessage("user", userContent)

		// Run agent sub-loop
		// Save LLM request content for /context current
		c.lastLLMSystemPrompt = dynamicPrompt
		c.lastLLMMessages = append([]provider.Message(nil), state.Messages...)
		if err := c.agent.runTurn(ctx, state, dynamicPrompt, io, c.agent.toolReg, c.loadedTools); err != nil {
			zap.S().Errorw("turn failed", "turn", state.Turn, "error", err)
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyTurnError), err))
		}

		// Post-turn: collect sub-agent results and synthesize.
		// In non-interactive transports (MQTT, Email) there is no "next user
		// input" to trigger the normal collection at the top of the loop, so
		// we must actively wait for and process results here. Interactive
		// transports are not affected because Collect() is non-blocking and
		// returns immediately when no agents are busy.
		c.collectAgentResults(ctx, state, io)

		// Flush any buffered output so the user receives the complete response.
		if err := io.Flush(); err != nil {
			zap.S().Warnw("flush failed after turn", "turn", state.Turn, "error", err)
		}

		// Capture cumulative token usage for /context display
		c.totalInputTokens = state.TotalInputTokens
		c.totalOutputTokens = state.TotalOutputTokens
		c.totalCachedTokens = state.TotalCachedTokens
		c.totalMissedTokens = state.TotalMissedTokens

		// Check for reload request
		if c.reloadRequested {
			state.StopReason = "reload"
			return
		}
	}
}

// collectAgentResults polls for completed sub-agent results and runs follow-up
// turns to synthesize them. Critical for non-interactive transports where
// there is no subsequent user input to trigger normal result collection.
//
// maxSynthesisRounds prevents infinite loops: when a synthesis turn dispatches
// new subagent tasks, agents remain busy and the loop would otherwise continue
// indefinitely dispatching → synthesizing → dispatching.
func (c *Coordinator) collectAgentResults(ctx context.Context, state *LoopState, io transport.UserIO) {
	if c.pool == nil {
		return
	}

	// Phase 1: Non-blocking drain — catch results completed during runTurn.
	c.drainAndSynthesize(ctx, state, io, false)

	// Yield to give pool worker goroutines a chance to start processing
	// freshly dispatched tasks. Without this, the coordinator can check
	// for busy agents before the worker has set status to "busy".
	runtime.Gosched()

	// Check if any agents are still busy
	agents := c.pool.List()
	if len(agents) == 0 {
		return
	}
	hasBusy := false
	for _, a := range agents {
		if a.Status == "busy" {
			hasBusy = true
			break
		}
	}
	if !hasBusy {
		return
	}

	// Phase 2: Active polling with synthesis round cap.
	timeout := time.Duration(c.agent.cfg.Pool.DefaultTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	const maxSynthesisRounds = 3
	synthesisRounds := 0

	for time.Now().Before(deadline) {
		// Check if all agents are done
		agents := c.pool.List()
		hasBusy := false
		for _, a := range agents {
			if a.Status == "busy" {
				hasBusy = true
				break
			}
		}
		if !hasBusy {
			c.drainAndSynthesize(ctx, state, io, false)
			return
		}

		// Cap reached — let the main loop handle remaining results.
		if synthesisRounds >= maxSynthesisRounds {
			zap.S().Debugw("max synthesis rounds reached",
				"rounds", synthesisRounds)
			break
		}

		// Poll interval
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return
		}

		if c.drainAndSynthesize(ctx, state, io, false) {
			synthesisRounds++
		}
	}

	// Timeout or cap reached — drain whatever is available.
	zap.S().Debugw("agent result collection poll ended",
		"timeout", timeout.Seconds(),
		"synthesis_rounds", synthesisRounds,
		"reason", map[bool]string{true: "timeout", false: "max_rounds"}[time.Now().After(deadline)])
	c.drainAndSynthesize(ctx, state, io, true)
}

// drainAndSynthesize drains completed results from the pool and runs a
// synthesis turn if any results were found. When timedOut is true, a
// timeout-specific message is used. Returns true if a synthesis turn
// was run. State.Messages is saved before and restored after to prevent
// synthesis context from leaking into subsequent user turns.
func (c *Coordinator) drainAndSynthesize(ctx context.Context, state *LoopState, io transport.UserIO, timedOut bool) bool {
	collected := c.pool.Collect()
	if len(collected) == 0 {
		return false
	}
	c.pending = append(c.pending, collected...)

	msg := "[Agent task results are now available. Synthesize them into your response.]"
	if timedOut {
		msg = "[Some agent task results are available (timed out waiting for others). Synthesize what you have.]"
	}

	dynamicPrompt := c.buildDynamicPrompt()
	c.lastLLMSystemPrompt = dynamicPrompt

	saved := state.Messages
	state.Messages = append(state.Messages, provider.Message{
		Role:    "user",
		Content: provider.TextContent(msg),
	})
	c.lastLLMMessages = append([]provider.Message(nil), state.Messages...)
	if err := c.agent.runTurn(ctx, state, dynamicPrompt, io, c.agent.toolReg, c.loadedTools); err != nil {
		zap.S().Debugw("agent result synthesis turn", "error", err)
	}
	state.Messages = saved
	return true
}

// buildDynamicPrompt returns the full system prompt including available agents
// and any pending results from completed agent tasks.
func (c *Coordinator) buildDynamicPrompt() string {
	var parts []string
	parts = append(parts, c.basePrompt)

	// Available agents
	agents := c.pool.List()
	if len(agents) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## Available Agents\n")
		for _, a := range agents {
			fmt.Fprintf(&sb, "- %s: %s\n", a.Name, a.Role)
		}
		parts = append(parts, sb.String())
	}

	// Available skills (top N by usage)
	if c.skills != nil {
		skills := c.skills.List() // use TopSkills when tracked
		if len(skills) > 0 {
			var sb strings.Builder
			sb.WriteString("\n## Available Skills\n")
			sb.WriteString("Skills are specialized capabilities you can load on demand with load_skill.\n")
			maxTop := c.agent.cfg.Skills.MaxTop
			if maxTop <= 0 {
				maxTop = 10
			}
			topSkills := c.skills.TopSkills(maxTop)
			for _, s := range topSkills {
				fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
			}
			if len(skills) > maxTop {
				fmt.Fprintf(&sb, "\n  [%d more skills available — use search_skills to find them]\n", len(skills)-maxTop)
			}
			parts = append(parts, sb.String())
		}
	}

	// Pending agent results
	if len(c.pending) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## Pending Agent Results\n")
		// Keep only last N results
		maxResults := c.agent.cfg.Pool.MaxPendingResults
		if maxResults <= 0 {
			maxResults = 10
		}
		start := 0
		if len(c.pending) > maxResults {
			start = len(c.pending) - maxResults
			fmt.Fprintf(&sb, "[%d older results omitted]\n", start)
		}
		for _, r := range c.pending[start:] {
			output := r.Output
			maxLen := c.agent.cfg.Pool.MaxPendingResultLen
			if maxLen > 0 && len(output) > maxLen {
				output = output[:maxLen] + "..."
			}
			statusIcon := "✓"
			if !r.Success {
				statusIcon = "✗"
			}
			fmt.Fprintf(&sb, "%s %s (%s): %s in %dms\n",
				statusIcon, r.AgentName, r.TaskID, r.Status, r.DurationMs)
			if output != "" {
				sb.WriteString("  " + strings.ReplaceAll(output, "\n", "\n  ") + "\n")
			} else if r.Error != "" {
				sb.WriteString("  error: " + r.Error + "\n")
			}
		}
		parts = append(parts, sb.String())
	}

	// SubSystems context
	if md := subsystem.ContextMD(); md != "" {
		parts = append(parts, md)
	}

	// Coordinator instructions
	parts = append(parts, `## Coordinator Instructions
You are a coordinator agent. Your job:
1. Handle simple requests directly using your tools.
2. For complex or specialized work, delegate to an available agent using dispatch_task.
3. If no existing agent fits, create a temporary one with create_agent.
4. Tasks dispatched to agents run asynchronously — you'll see results in the next turn.
5. Always synthesize multiple agent results into a coherent response for the user.
6. You can dispatch multiple tasks concurrently in a single turn.
7. Common MCP tools (shell, cdp, email, webhook) are pre-loaded when enabled. Use search_mcp_tools only when you need tools beyond these.`)

	return strings.Join(parts, "\n\n")
}

// parseCommandName extracts the command name from a /command line.
// "/analyze-competitor" -> "analyze-competitor"
// "/dev-run" -> "dev-run"
func parseCommandName(line string) string {
	if !strings.HasPrefix(line, "/") {
		return ""
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimPrefix(parts[0], "/")
}

// tryResumeSession checks for a previous session.
// Interactive transports (stdio, SSH): respect resume config + prompt user.
// Non-interactive transports (email, DingTalk, MQTT): always auto-resume
// to maintain continuous memory. Only /new starts a fresh session.
func (c *Coordinator) tryResumeSession(ctx context.Context, io transport.UserIO) (*session.Session, *LoopState) {
	caps := io.Capabilities()
	if caps.ShowToolDetails {
		// Interactive: require resume config + user confirmation
		if !c.agent.cfg.Session.Resume {
			return nil, nil
		}
	}
	// Non-interactive: always auto-resume

	id, path, turns, err := c.agent.sessMgr.LatestSession()
	if err != nil || id == "" {
		return nil, nil
	}

	if caps.ShowToolDetails {
		age := "unknown"
		if info, serr := os.Stat(path); serr == nil {
			age = formatDuration(time.Since(info.ModTime()))
		}
		zap.S().Infow("auto-resuming session", "id", id, "turns", turns, "age", age)
	}

	// Read and replay session events
	events, rerr := session.ReadEvents(path)
	if rerr != nil {
		zap.S().Warnw("failed to read session for resume", "path", path, "error", rerr)
		return nil, nil
	}

	messages := replayMessages(events)

	// Create a child session linked to the old one
	sess, rerr := c.agent.sessMgr.NewSessionWithParent(c.agent.cfg.Session.MaxLoop, id)
	if rerr != nil {
		zap.S().Errorw("create resumed session failed", "error", rerr)
		return nil, nil
	}

	zap.S().Infow("session resumed",
		"parent", id,
		"session_id", sess.ID,
		"messages", len(messages),
		"turns", turns,
	)

	return sess, &LoopState{
		Sess:     sess,
		Messages: messages,
		Turn:     turns,
	}
}

// replayMessages reconstructs the conversation Message list from session events.
func replayMessages(events []session.SessionEvent) []provider.Message {
	var msgs []provider.Message
	for _, evt := range events {
		switch evt.Type {
		case session.EventMessage:
			if evt.Role == "" || len(evt.Content) == 0 {
				continue
			}
			msgs = append(msgs, provider.Message{
				Role:    evt.Role,
				Content: evt.Content,
			})
		case session.EventToolResult:
			if len(evt.ToolResult) == 0 {
				continue
			}
			msgs = append(msgs, provider.Message{
				Role:    "tool",
				Content: evt.ToolResult,
			})
		}
	}
	return sanitizeToolPairing(msgs)
}

// sanitizeToolPairing ensures every assistant tool_use has a matching tool_result
// in the following messages. If a session was interrupted mid-tool-execution, the
// assistant message was logged but some tool results were not. Without this fix,
// the Anthropic API rejects the request with: "tool_use ids were found without
// tool_result blocks immediately after".
func sanitizeToolPairing(messages []provider.Message) []provider.Message {
	cleaned := make([]provider.Message, len(messages))
	copy(cleaned, messages)

	for i := 0; i < len(cleaned); i++ {
		if cleaned[i].Role != "assistant" {
			continue
		}
		toolIDs := extractToolUseIDs(cleaned[i].Content)
		if len(toolIDs) == 0 {
			continue
		}

		// Collect all tool_result IDs from consecutive tool messages after this assistant.
		found := make(map[string]bool)
		for j := i + 1; j < len(cleaned) && cleaned[j].Role == "tool"; j++ {
			for _, id := range extractToolResultIDs(cleaned[j].Content) {
				found[id] = true
			}
		}

		// If all matched, skip. Otherwise strip orphaned tool_use blocks.
		allFound := true
		for _, id := range toolIDs {
			if !found[id] {
				allFound = false
				break
			}
		}
		if !allFound {
			zap.S().Warnw("stripping orphaned tool_use blocks",
				"message_index", i,
				"tool_use_ids", toolIDs,
				"found_results", found,
			)
			cleaned[i].Content = stripOrphanedToolUses(cleaned[i].Content, found)
		}
	}
	return cleaned
}

func extractToolUseIDs(content json.RawMessage) []string {
	var blocks []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	var ids []string
	for _, b := range blocks {
		if b.Type == "tool_use" && b.ID != "" {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

func extractToolResultIDs(content json.RawMessage) []string {
	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	var ids []string
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			ids = append(ids, b.ToolUseID)
		}
	}
	return ids
}

func stripOrphanedToolUses(content json.RawMessage, validIDs map[string]bool) json.RawMessage {
	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return content
	}
	var cleaned []map[string]any
	for _, b := range blocks {
		if b["type"] == "tool_use" {
			id, _ := b["id"].(string)
			if !validIDs[id] {
				continue
			}
		}
		cleaned = append(cleaned, b)
	}
	result, _ := json.Marshal(cleaned)
	return json.RawMessage(result)
}

// formatDuration returns a human-readable relative duration.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// processDueTasks runs due cron tasks asynchronously.
func (c *Coordinator) processDueTasks(ctx context.Context, dueCh <-chan scheduler.CronTask, parentSessionID session.SessionID) {
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-dueCh:
			if !ok {
				return
			}
			if c.agent.hooks != nil {
				c.agent.hooks.Fire(ctx, hook.PointSchedulerTaskBefore, &hook.Context{
					SessionID: string(parentSessionID),
					TaskName:  task.Name,
					TaskInput: task.Task,
				})
			}
			result, err := c.agent.RunTask(ctx, task.Task, c.basePrompt, c.agent.toolReg, parentSessionID)
			if err != nil {
				c.cronMgr.AddResult(task.Name, false, "", err.Error())
				zap.S().Errorw("scheduled task failed", "name", task.Name, "error", err)
			} else {
				c.cronMgr.AddResult(task.Name, result.Success, result.Output, result.Error)
				zap.S().Infow("scheduled task completed", "name", task.Name, "task_id", result.TaskID)
			}
			if c.agent.hooks != nil {
				c.agent.hooks.Fire(ctx, hook.PointSchedulerTaskAfter, &hook.Context{
					SessionID: string(parentSessionID),
					TaskName:  task.Name,
					TaskInput: task.Task,
					Error:     err,
				})
			}
		}
	}
}
