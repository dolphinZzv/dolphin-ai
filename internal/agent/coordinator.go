package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/i18n"
	"dolphin/internal/mcp"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/transport"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

// Coordinator wraps an Agent with multi-agent coordination capabilities.
type Coordinator struct {
	*Agent
	pool             *AgentPool
	skills           *skill.Manager
	commands         *command.Manager
	cronMgr          *scheduler.Manager
	basePrompt       string
	pending          []TaskResult // results collected but not yet in LLM context
	startupRecommend *config.Recommendation
	loadedTools      map[string]bool // MCP tools loaded by LLM via load_mcp_tools
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
		comp = &DropCompressor{}
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
		availableProviders: agent.availableProviders,
		providerIndex:      agent.providerIndex,
	}
	// Core coordinator tools always available; MCP tools loaded on demand.
	coreTools := []string{"dispatch_task", "create_agent", "get_agent_status",
		"cancel_task", "delete_agent", "search_skills", "load_skill",
		"add_cron_task", "remove_cron_task", "list_cron_tasks", "toggle_cron_task",
		"config", "load_mcp_tools", "search_mcp_tools"}
	loaded := make(map[string]bool, len(coreTools))
	for _, name := range coreTools {
		loaded[name] = true
	}
	return &Coordinator{
		Agent:       coordAgent,
		pool:        pool,
		loadedTools: loaded,
	}
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

// SetStartupRecommend sets a recommendation to display on startup (async, non-blocking).
func (c *Coordinator) SetStartupRecommend(rec *config.Recommendation) {
	c.startupRecommend = rec
}

// Run starts the coordinator event loop.
func (c *Coordinator) Run(ctx context.Context, io transport.UserIO) {
	zap.S().Infow("coordinator starting")

	// Create or resume session
	var err error
	sess, state := c.tryResumeSession(ctx, io)
	if sess == nil {
		sess, err = c.sessMgr.NewSession(c.cfg.Session.MaxLoop)
		if err != nil {
			zap.S().Errorw("create session failed", "error", err)
			return
		}
		state = &LoopState{Sess: sess}
	}

	defer func() {
		// Fire session:end hook
		if c.hooks != nil {
			c.hooks.Fire(ctx, hook.PointSessionEnd, &hook.Context{
				SessionID: string(sess.ID),
				Turn:      state.Turn,
			})
		}
		c.generateSummary(sess, state)
		sess.Close()
		c.sessMgr.Remove(sess.ID)
		c.pool.Shutdown()
	}()

	// Build base system prompt
	c.ctxBuilder.SetRenderData(c.cfg)
	c.basePrompt, err = c.ctxBuilder.Build()
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

	// Link pool to coordinator session for sub-agent session tracing
	c.pool.SetParentSessionID(sess.ID)

	// Fire session:start hook
	if c.hooks != nil {
		c.hooks.Fire(ctx, hook.PointSessionStart, &hook.Context{
			SessionID: string(sess.ID),
			Turn:      0,
			Values:    make(map[string]any),
		})
	}

	zap.S().Debugw("coordinator session started",
		"session_id", sess.ID,
		"max_loop", c.cfg.Session.MaxLoop,
		"model", c.cfg.LLM.Model,
	)

	// Start cron task processor
	if c.cronMgr != nil {
		dueCh := c.cronMgr.Start(ctx)
		go c.processDueTasks(ctx, dueCh, sess.ID)
	}

	io.WriteLine(fmt.Sprintf("dolphin %s (%s/%s) built %s — Coordinator Ready", c.version, runtime.GOOS, runtime.Version(), c.buildTime))
	io.WriteLine(i18n.TL(i18n.KeyCoordReady))

	// Display async startup recommendation if ready
	if c.startupRecommend != nil && (len(c.startupRecommend.Skills) > 0 || len(c.startupRecommend.MCP) > 0) {
		io.WriteLine(config.PrintRecommendation(c.startupRecommend))
	}

	for {
		select {
		case <-ctx.Done():
			state.StopReason = "interrupted"
			return
		default:
		}

		// Check max loop
		if state.Turn >= c.cfg.Session.MaxLoop && !state.SummaryGenerated {
			state.SummaryGenerated = true
			zap.S().Infow("max loop reached, generating summary", "turns", state.Turn)
			c.generateSummary(sess, state)
			io.WriteLine(i18n.TL(i18n.KeySessionCheckpoint))
		}

		line, err := io.ReadLine()
		if err != nil {
			zap.S().Debugw("read line error", "error", err)
			state.StopReason = "transport_error"
			return
		}

		// Fire user:input hook (can rewrite or reject input)
		if c.hooks != nil {
			hc := &hook.Context{
				SessionID: string(sess.ID),
				Turn:      state.Turn + 1,
				UserInput: line,
				Values:    make(map[string]any),
			}
			if err := c.hooks.Fire(ctx, hook.PointUserInput, hc); err != nil {
				io.WriteLine(fmt.Sprintf("[Rejected: %v]", err))
				continue
			}
			line = hc.UserInput
		}

		// Handle commands
		switch {
		case line == "/exit":
			state.StopReason = "user_exit"
			return
		case line == "/help":
			c.printHelp(io)
			continue
		case line == "/status":
			c.handleStatus(sess, state, io)
			continue
		case line == "/sessions":
			c.handleSessions(io)
			continue
		case line == "/mcp":
			c.printMCP(io)
			continue
		case line == "/agents":
			c.printAgents(io)
			continue
		case strings.HasPrefix(line, "/skills new"):
			c.handleSkillNew(line, io)
			continue
		case line == "/skills":
			c.printSkills(io)
			continue
		case line == "/new":
			c.handleNew(sess, state, io)
			continue
		case line == "/commands":
			c.printCommands(io)
			continue
		case line == "/crontab":
			c.printCronTasks(io)
			continue
		case line == "/model" || strings.HasPrefix(line, "/model "):
			c.handleModelCmd(line, io)
			continue
		case strings.HasPrefix(line, "/cancel"):
			c.handleCancelCmd(line, io)
			continue
		case line == "":
			continue
		}

		// Check user-defined /commands (matched after built-in commands)
		if c.commands != nil && strings.HasPrefix(line, "/") {
			if cmdName := parseCommandName(line); cmdName != "" {
				if cmd, ok := c.commands.Get(cmdName); ok {
					c.commands.RecordUsage(cmdName)
					var sb strings.Builder
					sb.WriteString("User triggered command /")
					sb.WriteString(cmdName)
					sb.WriteString("\n\n")
					sb.WriteString(cmd.Content)
					args := strings.TrimSpace(line[len("/"+cmdName):])
					if args != "" {
						sb.WriteString("\n\nUser arguments: ")
						sb.WriteString(args)
					}
					line = sb.String()
				}
			}
		}

		state.Turn++
		sess.Turn = state.Turn

		// Collect pending agent results
		collected := c.pool.Collect()
		c.pending = append(c.pending, collected...)
		// Emit agent:completed events
		if c.events != nil {
			for _, r := range collected {
				c.events.Emit(ctx, event.Event{
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
		maxResults := c.cfg.Pool.MaxPendingResults
		if maxResults <= 0 {
			maxResults = 10
		}
		if len(c.pending) > maxResults*2 {
			c.pending = c.pending[len(c.pending)-maxResults:]
		}

		// Build dynamic system prompt with current context
		dynamicPrompt := c.buildDynamicPrompt()

		// Add user message
		userContent := TextContent(line)
		state.Messages = append(state.Messages, Message{Role: "user", Content: userContent})
		sess.LogMessage("user", userContent)

		// Run agent sub-loop
		if err := c.runTurn(ctx, state, dynamicPrompt, io, c.toolReg, c.loadedTools); err != nil {
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
	}
}

// collectAgentResults polls for completed sub-agent results and runs follow-up
// turns to synthesize them. Critical for non-interactive transports where
// there is no subsequent user input to trigger normal result collection.
func (c *Coordinator) collectAgentResults(ctx context.Context, state *LoopState, io transport.UserIO) {
	if c.pool == nil {
		return
	}

	timeout := time.Duration(c.cfg.Pool.DefaultTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Non-blocking collect
		collected := c.pool.Collect()
		if len(collected) > 0 {
			c.pending = append(c.pending, collected...)

			// Run a follow-up turn to synthesize the results
			dynamicPrompt := c.buildDynamicPrompt()
			state.Messages = append(state.Messages, Message{
				Role:    "user",
				Content: TextContent("[Agent task results are now available. Synthesize them into your response.]"),
			})

			if err := c.runTurn(ctx, state, dynamicPrompt, io, c.toolReg, c.loadedTools); err != nil {
				zap.S().Debugw("agent result follow-up turn", "error", err)
				return
			}

			// Reset deadline after processing — more results may follow
			deadline = time.Now().Add(timeout)
		}

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

		// Poll interval
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}

	// Timeout reached — drain whatever is available
	zap.S().Debugw("agent result collection timed out", "timeout", timeout.Seconds())
	collected := c.pool.Collect()
	if len(collected) > 0 {
		c.pending = append(c.pending, collected...)
		dynamicPrompt := c.buildDynamicPrompt()
		state.Messages = append(state.Messages, Message{
			Role:    "user",
			Content: TextContent("[Some agent task results are available (timed out waiting for others). Synthesize what you have.]"),
		})
		c.runTurn(ctx, state, dynamicPrompt, io, c.toolReg, c.loadedTools)
	}
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
			sb.WriteString(fmt.Sprintf("- %s [%s]: %s\n", a.Name, a.Status, a.Role))
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
			maxTop := c.cfg.Skills.MaxTop
			if maxTop <= 0 {
				maxTop = 10
			}
			topSkills := c.skills.TopSkills(maxTop)
			for _, s := range topSkills {
				usage := ""
				if s.CallCount > 0 {
					usage = fmt.Sprintf(" (used %d times)", s.CallCount)
				}
				sb.WriteString(fmt.Sprintf("- %s: %s%s\n", s.Name, s.Description, usage))
			}
			if len(skills) > maxTop {
				sb.WriteString(fmt.Sprintf("\n  [%d more skills available — use search_skills to find them]\n", len(skills)-maxTop))
			}
			parts = append(parts, sb.String())
		}
	}

	// Pending agent results
	if len(c.pending) > 0 {
		var sb strings.Builder
		sb.WriteString("\n## Pending Agent Results\n")
		// Keep only last N results
		maxResults := c.cfg.Pool.MaxPendingResults
		if maxResults <= 0 {
			maxResults = 10
		}
		start := 0
		if len(c.pending) > maxResults {
			start = len(c.pending) - maxResults
			sb.WriteString(fmt.Sprintf("[%d older results omitted]\n", start))
		}
		for _, r := range c.pending[start:] {
			output := r.Output
			maxLen := c.cfg.Pool.MaxPendingResultLen
			if maxLen > 0 && len(output) > maxLen {
				output = output[:maxLen] + "..."
			}
			statusIcon := "✓"
			if !r.Success {
				statusIcon = "✗"
			}
			sb.WriteString(fmt.Sprintf("%s %s (%s): %s in %dms\n",
				statusIcon, r.AgentName, r.TaskID, r.Status, r.DurationMs))
			if output != "" {
				sb.WriteString("  " + strings.ReplaceAll(output, "\n", "\n  ") + "\n")
			} else if r.Error != "" {
				sb.WriteString("  error: " + r.Error + "\n")
			}
		}
		parts = append(parts, sb.String())
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
7. MCP tools are not loaded by default. Use search_mcp_tools to discover available tools, then load_mcp_tools to add them to your tool list for subsequent turns.`)

	return strings.Join(parts, "\n\n")
}

// registerCoordinatorTools adds coordinator-only tools to the agent registry.
func (c *Coordinator) registerCoordinatorTools() {
	c.registerCoordTool("dispatch_task",
		"Dispatch a task to a specialized agent for async processing. The agent will process it and you'll see the result in your next turn.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent":   map[string]any{"type": "string", "description": "Target agent name"},
				"task":    map[string]any{"type": "string", "description": "Detailed task description"},
				"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (optional)"},
			},
			"required": []string{"agent", "task"},
		},
		c.handleDispatchTask,
	)
	c.registerCoordTool("create_agent",
		"Create a temporary agent for a novel task. Use this when no existing agent fits the user's request.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Agent name"},
				"role":    map[string]any{"type": "string", "description": "Role description for the agent's system prompt"},
				"tools":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tool allowlist (default: all)"},
				"model":   map[string]any{"type": "string", "description": "Model override (optional)"},
				"timeout": map[string]any{"type": "integer", "description": "Task timeout in seconds (optional)"},
			},
			"required": []string{"name", "role"},
		},
		c.handleCreateAgent,
	)
	c.registerCoordTool("get_agent_status",
		"Get the status of all agents or a specific agent.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent": map[string]any{"type": "string", "description": "Agent name (optional, empty = all)"},
			},
		},
		c.handleGetAgentStatus,
	)
	c.registerCoordTool("cancel_task",
		"Cancel a running task by its task ID.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to cancel"},
			},
			"required": []string{"task_id"},
		},
		c.handleCancelTask,
	)
	c.registerCoordTool("delete_agent",
		"Delete a temporary agent and clean up its workspace.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Agent name to delete"},
			},
			"required": []string{"name"},
		},
		c.handleDeleteAgent,
	)
	c.registerCoordTool("search_mcp_tools",
		"Search available MCP tools by name or description. Use this when you need a tool not in your current tool list.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query (matched against tool name and description)"},
			},
			"required": []string{"query"},
		},
		c.handleSearchMCPTools,
	)
	c.registerCoordTool("search_skills",
		"Search available skills by name or description. Skills are specialized capabilities that can be loaded for detailed instructions.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query (matched against skill name and description)"},
			},
			"required": []string{"query"},
		},
		c.handleSearchSkills,
	)
	c.registerCoordTool("load_skill",
		"Load a skill's full content. Use this when you need the detailed instructions for a specific skill (e.g., for code review, data analysis).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name to load"},
			},
			"required": []string{"name"},
		},
		c.handleLoadSkill,
	)
	c.registerCoordTool("add_cron_task",
		"Add a scheduled task that runs on a cron schedule.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Unique task name"},
				"schedule":    map[string]any{"type": "string", "description": "5-field cron expression (e.g. \"0 18 * * 1-5\")"},
				"description": map[string]any{"type": "string", "description": "Human-readable description"},
				"task":        map[string]any{"type": "string", "description": "Instructions for the agent when task runs"},
			},
			"required": []string{"name", "schedule", "description", "task"},
		},
		c.handleAddCronTask,
	)
	c.registerCoordTool("remove_cron_task",
		"Remove a scheduled task by name.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Task name to remove"},
			},
			"required": []string{"name"},
		},
		c.handleRemoveCronTask,
	)
	c.registerCoordTool("list_cron_tasks",
		"List all scheduled tasks with their status and schedule.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		c.handleListCronTasks,
	)
	c.registerCoordTool("toggle_cron_task",
		"Enable or disable a scheduled task.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Task name"},
				"enabled": map[string]any{"type": "boolean", "description": "true to enable, false to disable"},
			},
			"required": []string{"name", "enabled"},
		},
		c.handleToggleCronTask,
	)
	c.registerCoordTool("config",
		"Read and modify runtime configuration. Actions: list (show all settings), get (read a path), set (modify a setting), save (persist to disk). Changes to MCP tool settings (shell/cdp/email/webhook) take effect immediately. Changes to LLM settings take effect on the next conversation turn.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "get", "set", "save"},
					"description": "Action: list (show all settings), get (read a path), set (modify a path), save (persist to disk)",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Config path in dot notation, e.g. mcp.shell.timeout_seconds, llm.temperature. Use list to see all paths.",
				},
				"value": map[string]any{
					"description": "New value for the setting (used with set action). Type depends on the setting.",
				},
				"file": map[string]any{
					"type":        "string",
					"description": "Target file path for save action (optional, defaults to .dolphin/config.yaml)",
				},
			},
			"required": []string{"action"},
		},
		c.handleConfig,
	)
	c.registerCoordTool("load_mcp_tools",
		"Load MCP tools by name so they become available for use as API-level tools. Use search_mcp_tools first to discover available tool names. Loaded tools will appear in the tool list starting from your next turn.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tools": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Names of MCP tools to load. Use search_mcp_tools to find available tools.",
				},
			},
			"required": []string{"tools"},
		},
		c.handleLoadMCPTools,
	)
}

func (c *Coordinator) registerCoordTool(name, description string, schema map[string]any, handler func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error)) {
	schemaJSON, _ := json.Marshal(schema)
	c.toolReg.Register(&handlerTool{
		def: mcp.ToolDefinition{
			Name:        name,
			Description: description,
			InputSchema: schemaJSON,
		},
		handler: handler,
	})
	zap.S().Debugw("coordinator tool registered", "tool", name)
}

// ---- Tool handlers ----

func (c *Coordinator) handleDispatchTask(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Agent   string `json:"agent"`
		Task    string `json:"task"`
		Timeout int    `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	task := Task{
		ID:      xid.New().String(),
		Input:   params.Task,
		Timeout: params.Timeout,
	}

	if err := c.pool.Dispatch(params.Agent, task); err != nil {
		return &mcp.ToolResult{Content: "dispatch failed: " + err.Error(), IsError: true}, nil
	}

	if c.events != nil {
		c.events.Emit(ctx, event.Event{
			Type:      event.TypeAgentDispatched,
			SessionID: string(c.pool.ParentSessionID()),
			Data: map[string]any{
				"agent":   params.Agent,
				"task_id": task.ID,
			},
		})
	}

	result := fmt.Sprintf("Task dispatched to %s (task_id: %s). The agent is processing it asynchronously.",
		params.Agent, task.ID)
	return &mcp.ToolResult{Content: result}, nil
}

func (c *Coordinator) handleCreateAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name    string   `json:"name"`
		Role    string   `json:"role"`
		Tools   []string `json:"tools,omitempty"`
		Model   string   `json:"model,omitempty"`
		Timeout int      `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	// Check if agent already exists
	for _, a := range c.pool.List() {
		if a.Name == params.Name {
			return &mcp.ToolResult{
				Content: fmt.Sprintf("Agent %s already exists (status: %s). Use dispatch_task to send it work.", params.Name, a.Status),
			}, nil
		}
	}

	workspace := TempAgentWorkspace(&c.cfg.Pool, params.Name)
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = c.cfg.Pool.DefaultTimeout
	}

	def := &AgentDef{
		Name:      params.Name,
		Role:      params.Role,
		Tools:     params.Tools,
		Model:     params.Model,
		Workspace: workspace,
		Timeout:   timeout,
	}

	c.pool.Add(params.Name, def, AgentCoord, c.Agent, c.toolReg)

	result := fmt.Sprintf("Temporary agent '%s' created (workspace: %s). Use dispatch_task to send it work.",
		params.Name, workspace)
	return &mcp.ToolResult{Content: result}, nil
}

func (c *Coordinator) handleGetAgentStatus(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Agent string `json:"agent,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	agents := c.pool.List()
	if params.Agent != "" {
		for _, a := range agents {
			if a.Name == params.Agent {
				return &mcp.ToolResult{
					Content: fmt.Sprintf("Agent: %s\nType: %s\nStatus: %s\nTasks done: %d\nWorkspace: %s",
						a.Name, a.Kind, a.Status, a.TasksDone, a.Workspace),
				}, nil
			}
		}
		return &mcp.ToolResult{
			Content: fmt.Sprintf("Agent not found: %s", params.Agent),
			IsError: true,
		}, nil
	}

	if len(agents) == 0 {
		return &mcp.ToolResult{Content: "No agents available."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d agent(s) available:\n", len(agents)))
	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("- %s [%s] [%s] tasks: %d\n", a.Name, a.Status, a.Kind, a.TasksDone))
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) handleCancelTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	if c.pool.Cancel(params.TaskID) {
		return &mcp.ToolResult{Content: fmt.Sprintf("Task %s cancelled.", params.TaskID)}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("No running task found with ID: %s", params.TaskID), IsError: true}, nil
}

func (c *Coordinator) handleDeleteAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if c.pool.Remove(params.Name) {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent '%s' deleted.", params.Name)}, nil
	}
	return &mcp.ToolResult{
		Content: fmt.Sprintf("Agent not found: %s", params.Name),
		IsError: true,
	}, nil
}

// ---- MCP tool search handler ----

func (c *Coordinator) handleSearchMCPTools(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	defs := c.toolReg.SearchTools(params.Query)
	if len(defs) == 0 {
		return &mcp.ToolResult{Content: fmt.Sprintf("No MCP tools found matching %q.", params.Query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d MCP tool(s) matching %q:\n", len(defs), params.Query))
	for _, d := range defs {
		stats := c.toolReg.ToolStats()
		usage := ""
		if s, ok := stats[d.Name]; ok && s.CallCount > 0 {
			usage = fmt.Sprintf(" (used %d times)", s.CallCount)
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", d.Name, d.Description, usage))
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

// handleLoadMCPTools loads MCP tools by name into the LLM's active tool set for the next turn.
func (c *Coordinator) handleLoadMCPTools(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if len(params.Tools) == 0 {
		return &mcp.ToolResult{Content: "No tool names provided.", IsError: true}, nil
	}

	var loaded, notFound []string
	for _, name := range params.Tools {
		if _, ok := c.toolReg.Get(name); ok {
			c.loadedTools[name] = true
			loaded = append(loaded, name)
		} else {
			notFound = append(notFound, name)
		}
	}

	var sb strings.Builder
	if len(loaded) > 0 {
		sb.WriteString(fmt.Sprintf("Loaded %d tool(s): %s\n", len(loaded), strings.Join(loaded, ", ")))
		sb.WriteString("They will be available in the tool list starting from your next turn.")
	}
	if len(notFound) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("Tool(s) not found: %s. Use search_mcp_tools to discover available tools.", strings.Join(notFound, ", ")))
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

// ---- Skill handlers ----

func (c *Coordinator) handleSearchSkills(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.skills == nil {
		return &mcp.ToolResult{Content: "Skills system is not available.", IsError: true}, nil
	}

	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	results := c.skills.Search(params.Query)
	if len(results) == 0 {
		return &mcp.ToolResult{Content: fmt.Sprintf("No skills found matching %q.", params.Query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skill(s) matching %q:\n", len(results), params.Query))
	for _, s := range results {
		usage := ""
		if s.CallCount > 0 {
			usage = fmt.Sprintf(" (used %d times)", s.CallCount)
		}
		sb.WriteString(fmt.Sprintf("- %s: %s%s\n", s.Name, s.Description, usage))
	}
	sb.WriteString("\nUse load_skill to load the full content of a skill.")
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) handleLoadSkill(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.skills == nil {
		return &mcp.ToolResult{Content: "Skills system is not available.", IsError: true}, nil
	}

	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	s, ok := c.skills.Get(params.Name)
	if !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q not found. Use search_skills to find available skills.", params.Name), IsError: true}, nil
	}

	c.skills.RecordUsage(params.Name)

	if c.events != nil {
		c.events.Emit(ctx, event.Event{
			Type:      event.TypeSkillLoaded,
			SessionID: string(c.pool.ParentSessionID()),
			Data:      map[string]any{"skill": params.Name},
		})
	}

	result := fmt.Sprintf("# Skill: %s\n\n%s\n\n---\nLoaded skill %q. Use these instructions to guide your work.", s.Name, s.Content, s.Name)
	return &mcp.ToolResult{Content: result}, nil
}

// ---- Cron task handlers ----

func (c *Coordinator) handleAddCronTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.cronMgr == nil {
		return &mcp.ToolResult{Content: "Cron scheduler not available.", IsError: true}, nil
	}
	var params struct {
		Name        string `json:"name"`
		Schedule    string `json:"schedule"`
		Description string `json:"description"`
		Task        string `json:"task"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	task := &scheduler.CronTask{
		Name:        params.Name,
		Schedule:    params.Schedule,
		Description: params.Description,
		Enabled:     true,
		Task:        params.Task,
	}
	if err := c.cronMgr.AddTask(task); err != nil {
		return &mcp.ToolResult{Content: "add cron task: " + err.Error(), IsError: true}, nil
	}
	return &mcp.ToolResult{
		Content: fmt.Sprintf("Scheduled task %q created (schedule: %s). It will run automatically at the specified times.", params.Name, params.Schedule),
	}, nil
}

func (c *Coordinator) handleRemoveCronTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.cronMgr == nil {
		return &mcp.ToolResult{Content: "Cron scheduler not available.", IsError: true}, nil
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if c.cronMgr.RemoveTask(params.Name) {
		return &mcp.ToolResult{Content: fmt.Sprintf("Scheduled task %q removed.", params.Name)}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Scheduled task %q not found.", params.Name), IsError: true}, nil
}

func (c *Coordinator) handleListCronTasks(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.cronMgr == nil {
		return &mcp.ToolResult{Content: "Cron scheduler not available.", IsError: true}, nil
	}
	tasks := c.cronMgr.List()
	if len(tasks) == 0 {
		return &mcp.ToolResult{Content: "No scheduled tasks. Use add_cron_task to create one."}, nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d scheduled task(s):\n", len(tasks)))
	for _, t := range tasks {
		status := "enabled"
		if !t.Enabled {
			status = "disabled"
		}
		sb.WriteString(fmt.Sprintf("- %s [%s] %s (%s)\n", t.Name, status, t.Schedule, t.Description))
	}
	results := c.cronMgr.PendingResults()
	if len(results) > 0 {
		sb.WriteString("\nRecent results:\n")
		for _, r := range results {
			mark := "✓"
			if !r.Success {
				mark = "✗"
			}
			msg := r.Output
			if r.Error != "" {
				msg = r.Error
			}
			if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s %s: %s\n", mark, r.TaskName, msg))
		}
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) handleToggleCronTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.cronMgr == nil {
		return &mcp.ToolResult{Content: "Cron scheduler not available.", IsError: true}, nil
	}
	var params struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if c.cronMgr.ToggleTask(params.Name, params.Enabled) {
		state := "disabled"
		if params.Enabled {
			state = "enabled"
		}
		return &mcp.ToolResult{Content: fmt.Sprintf("Scheduled task %q %s.", params.Name, state)}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Scheduled task %q not found.", params.Name), IsError: true}, nil
}

// ---- Commands ----

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
	if len(agents) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoAgents))
		io.WriteLine(i18n.TL(i18n.KeyNoAgentsHint))
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyAgentHeader), "AGENT", "STATUS", "TYPE", "TASKS"))
	io.WriteLine("------------------------------------------------")
	for _, a := range agents {
		io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %d",
			a.Name, a.Status, a.Kind, a.TasksDone))
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
	io.WriteLine("")
}

func (c *Coordinator) handleSkillNew(line string, io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine(i18n.TL(i18n.KeySkillsNotAvail))
		return
	}

	// Parse: "/skills new <name>"
	rest := strings.TrimPrefix(line, "/skills new")
	rest = strings.TrimSpace(rest)

	if rest == "" {
		io.WriteLine(i18n.TL(i18n.KeySkillNewUsage))
		return
	}

	name := rest

	if err := c.skills.NewTemplate(name, ""); err != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillNewError), err))
		return
	}

	dir := c.skills.Dir()
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeySkillNewCreated), name, dir))
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

func (c *Coordinator) handleModelCmd(line string, io transport.UserIO) {
	providers := c.availableProviders
	if len(providers) == 0 {
		io.WriteLine("No providers configured")
		return
	}

	parts := strings.Fields(line)
	if len(parts) == 1 {
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
	name := parts[1]
	if c.switchToProvider(name) {
		io.WriteLine(fmt.Sprintf("Switched to %s (%s)", name, c.provider.Name()))
	} else {
		io.WriteLine(fmt.Sprintf("Provider %q not found or unhealthy", name))
	}
}

func (c *Coordinator) handleCancelCmd(line string, io transport.UserIO) {
	parts := strings.Fields(line)
	if len(parts) > 1 {
		taskID := parts[1]
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

func (c *Coordinator) handleNew(sess *session.Session, state *LoopState, io transport.UserIO) {
	oldTurns := state.Turn
	oldID := sess.ID

	// Generate summary for current session before starting fresh
	c.generateSummary(sess, state)

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

	zap.S().Infow("session reset via /new",
		"old_session", oldID,
		"new_session", newSess.ID,
	)

	io.WriteLine(fmt.Sprintf("New session %s started. Previous: %s (%d turns)",
		newSess.ID, oldID, oldTurns,
	))
	io.WriteLine("")
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
		if !c.cfg.Session.Resume {
			return nil, nil
		}
	}
	// Non-interactive: always auto-resume

	id, path, turns, err := c.sessMgr.LatestSession()
	if err != nil || id == "" {
		return nil, nil
	}

	if caps.ShowToolDetails {
		// Interactive transport: prompt the user
		age := "unknown"
		if info, serr := os.Stat(path); serr == nil {
			age = formatDuration(time.Since(info.ModTime()))
		}

		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyResumePrompt), id, turns, age))
		line, rerr := io.ReadLine()
		if rerr != nil || (line != "" && line != "y" && line != "Y" && line != strings.ToLower(i18n.TL(i18n.KeyResumeYes))) {
			io.WriteLine("")
			return nil, nil
		}
		io.WriteLine("")
	}

	// Read and replay session events
	events, rerr := session.ReadEvents(path)
	if rerr != nil {
		zap.S().Warnw("failed to read session for resume", "path", path, "error", rerr)
		return nil, nil
	}

	messages := replayMessages(events)

	// Create a child session linked to the old one
	sess, rerr := c.sessMgr.NewSessionWithParent(c.cfg.Session.MaxLoop, id)
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
func replayMessages(events []session.SessionEvent) []Message {
	var msgs []Message
	for _, evt := range events {
		switch evt.Type {
		case session.EventMessage:
			if evt.Role == "" || len(evt.Content) == 0 {
				continue
			}
			msgs = append(msgs, Message{
				Role:    evt.Role,
				Content: evt.Content,
			})
		case session.EventToolResult:
			if len(evt.ToolResult) == 0 {
				continue
			}
			msgs = append(msgs, Message{
				Role:    "tool",
				Content: evt.ToolResult,
			})
		}
	}
	return msgs
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
			result, err := c.RunTask(ctx, task.Task, c.basePrompt, c.toolReg, parentSessionID)
			if err != nil {
				c.cronMgr.AddResult(task.Name, false, "", err.Error())
				zap.S().Errorw("scheduled task failed", "name", task.Name, "error", err)
			} else {
				c.cronMgr.AddResult(task.Name, result.Success, result.Output, result.Error)
				zap.S().Infow("scheduled task completed", "name", task.Name, "task_id", result.TaskID)
			}
		}
	}
}

// handlerTool wraps a function as an MCP Tool.
type handlerTool struct {
	def     mcp.ToolDefinition
	handler func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error)
}

func (t *handlerTool) Definition() mcp.ToolDefinition { return t.def }

func (t *handlerTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	return t.handler(ctx, input)
}
