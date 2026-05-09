package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/skill"
	"dolphinzZ/internal/transport"

	"github.com/rs/xid"
)

// Coordinator wraps an Agent with multi-agent coordination capabilities.
type Coordinator struct {
	*Agent
	pool       *AgentPool
	skills     *skill.Manager
	basePrompt string
	pending    []TaskResult // results collected but not yet in LLM context
}

// NewCoordinator creates a coordinator from an existing Agent and agent pool.
// The tool registry is cloned so that coordinator tool registration (dispatch_task,
// create_agent, etc.) does not overwrite handlers on the shared registry across
// multiple transport connections.
func NewCoordinator(agent *Agent, pool *AgentPool) *Coordinator {
	// Clone the tool registry for per-coordinator tool registration
	coordReg := agent.toolReg.Clone()
	coordAgent := &Agent{
		cfg:        agent.cfg,
		sessMgr:    agent.sessMgr,
		toolReg:    coordReg,
		provider:   agent.provider,
		ctxBuilder: agent.ctxBuilder,
	}
	return &Coordinator{
		Agent: coordAgent,
		pool:  pool,
	}
}

// SetSkillManager sets the skill manager for skills support.
// Should be called before Run().
func (c *Coordinator) SetSkillManager(mgr *skill.Manager) {
	c.skills = mgr
}

// Run starts the coordinator event loop.
func (c *Coordinator) Run(ctx context.Context, io transport.UserIO) {
	slog.Info("coordinator starting")

	// Create session
	sess, err := c.sessMgr.NewSession(c.cfg.Session.MaxLoop)
	if err != nil {
		slog.Error("create session failed", "error", err)
		return
	}

	state := &LoopState{Sess: sess}

	defer func() {
		c.generateSummary(sess, state)
		sess.Close()
		c.sessMgr.Remove(sess.ID)
		c.pool.Shutdown()
	}()

	// Build base system prompt
	c.basePrompt, err = c.ctxBuilder.Build()
	if err != nil {
		slog.Error("build context failed", "error", err)
		return
	}

	// Register coordinator tools on the agent's tool registry
	c.registerCoordinatorTools()

	// Link pool to coordinator session for sub-agent session tracing
	c.pool.SetParentSessionID(sess.ID)

	slog.Debug("coordinator session started",
		"session_id", sess.ID,
		"max_loop", c.cfg.Session.MaxLoop,
		"model", c.cfg.LLM.Model,
	)

	io.WriteLine("DolphinzZ Coordinator ready. Type /exit to quit, /help for help, /agents to list agents.")
	io.WriteLine("")

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
			slog.Info("max loop reached, generating summary", "turns", state.Turn)
			c.generateSummary(sess, state)
			io.WriteLine("\n[Session checkpoint: summary saved, continuing...]\n")
		}

		line, err := io.ReadLine()
		if err != nil {
			slog.Debug("read line error", "error", err)
			return
		}

		// Handle commands
		switch {
		case line == "/exit" || line == "/quit":
			state.StopReason = "user_exit"
			return
		case line == "/help":
			c.printHelp(io)
			continue
		case line == "/agents":
			c.printAgents(io)
			continue
		case line == "/skills":
			c.printSkills(io)
			continue
		case strings.HasPrefix(line, "/cancel"):
			c.handleCancelCmd(line, io)
			continue
		case line == "":
			continue
		}

		state.Turn++
		sess.Turn = state.Turn

		// Collect pending agent results
		c.pending = append(c.pending, c.pool.Collect()...)

		// Build dynamic system prompt with current context
		dynamicPrompt := c.buildDynamicPrompt()

		// Add user message
		userContent := TextContent(line)
		state.Messages = append(state.Messages, Message{Role: "user", Content: userContent})
		sess.LogMessage("user", userContent)

		// Run agent sub-loop
		if err := c.runTurn(ctx, state, dynamicPrompt, io, c.toolReg); err != nil {
			slog.Error("turn failed", "turn", state.Turn, "error", err)
			io.WriteLine(fmt.Sprintf("\n[Error: %v]", err))
		}
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
			if len(output) > 500 {
				output = output[:500] + "..."
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
7. Available MCP tools are shown as top tools. If you need one not in the list, use search_mcp_tools to find it.`)

	return strings.Join(parts, "\n\n")
}

// registerCoordinatorTools adds coordinator-only tools to the agent registry.
func (c *Coordinator) registerCoordinatorTools() {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error)
	}{
		{
			name:        "dispatch_task",
			description: "Dispatch a task to a specialized agent for async processing. The agent will process it and you'll see the result in your next turn.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent":   map[string]any{"type": "string", "description": "Target agent name"},
					"task":    map[string]any{"type": "string", "description": "Detailed task description"},
					"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (optional)"},
				},
				"required": []string{"agent", "task"},
			},
			handler: c.handleDispatchTask,
		},
		{
			name:        "create_agent",
			description: "Create a temporary agent for a novel task. Use this when no existing agent fits the user's request.",
			schema: map[string]any{
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
			handler: c.handleCreateAgent,
		},
		{
			name:        "get_agent_status",
			description: "Get the status of all agents or a specific agent.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent": map[string]any{"type": "string", "description": "Agent name (optional, empty = all)"},
				},
			},
			handler: c.handleGetAgentStatus,
		},
		{
			name:        "cancel_task",
			description: "Cancel a running task by its task ID.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "Task ID to cancel"},
				},
				"required": []string{"task_id"},
			},
			handler: c.handleCancelTask,
		},
		{
			name:        "delete_agent",
			description: "Delete a temporary agent and clean up its workspace.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Agent name to delete"},
				},
				"required": []string{"name"},
			},
			handler: c.handleDeleteAgent,
		},
		{
			name:        "search_mcp_tools",
			description: "Search available MCP tools by name or description. Use this when you need a tool not in your current tool list.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query (matched against tool name and description)"},
				},
				"required": []string{"query"},
			},
			handler: c.handleSearchMCPTools,
		},
		{
			name:        "search_skills",
			description: "Search available skills by name or description. Skills are specialized capabilities that can be loaded for detailed instructions.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query (matched against skill name and description)"},
				},
				"required": []string{"query"},
			},
			handler: c.handleSearchSkills,
		},
		{
			name:        "load_skill",
			description: "Load a skill's full content. Use this when you need the detailed instructions for a specific skill (e.g., for code review, data analysis).",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name to load"},
				},
				"required": []string{"name"},
			},
			handler: c.handleLoadSkill,
		},
	}

	for _, t := range tools {
		schema, _ := json.Marshal(t.schema)
		ht := &handlerTool{
			def: mcp.ToolDefinition{
				Name:        t.name,
				Description: t.description,
				InputSchema: schema,
			},
			handler: t.handler,
		}
		c.toolReg.Register(ht)
		slog.Debug("coordinator tool registered", "tool", t.name)
	}
}

// ---- Tool handlers ----

func (c *Coordinator) handleDispatchTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
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
	json.Unmarshal(input, &params)

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

func (c *Coordinator) handleLoadSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
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

	result := fmt.Sprintf("# Skill: %s\n\n%s\n\n---\nLoaded skill %q. Use these instructions to guide your work.", s.Name, s.Content, s.Name)
	return &mcp.ToolResult{Content: result}, nil
}

// ---- Commands ----

func (c *Coordinator) printHelp(io transport.UserIO) {
	io.WriteLine("Commands:")
	io.WriteLine("  /exit, /quit  - Exit")
	io.WriteLine("  /help         - This help")
	io.WriteLine("  /agents       - List available agents and their status")
	io.WriteLine("  /skills       - List available skills")
	io.WriteLine("  /cancel       - Cancel all running tasks")
	io.WriteLine("  /cancel <id>  - Cancel a specific task by ID")
	io.WriteLine("")
	io.WriteLine("Top MCP tools (by usage, use search_mcp_tools to find more):")
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
			io.WriteLine(fmt.Sprintf("\nSkills: %d available (use /skills to list, search_skills to find)", len(skills)))
		}
	}
	io.WriteLine("")
}

func (c *Coordinator) printAgents(io transport.UserIO) {
	agents := c.pool.List()
	if len(agents) == 0 {
		// Check if pool is empty because agents/ dir doesn't exist
		io.WriteLine("No agents configured. Create agents in .dolphinzZ/agents/<name>/agent.yaml")
		return
	}

	io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %s", "AGENT", "STATUS", "TYPE", "TASKS"))
	io.WriteLine("------------------------------------------------")
	for _, a := range agents {
		io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %d",
			a.Name, a.Status, a.Kind, a.TasksDone))
	}
	io.WriteLine("")
}

func (c *Coordinator) printSkills(io transport.UserIO) {
	if c.skills == nil {
		io.WriteLine("Skills system not available. Create skills in .dolphinzZ/skills/")
		return
	}

	skills := c.skills.List()
	if len(skills) == 0 {
		io.WriteLine("No skills found. Add .md files to .dolphinzZ/skills/")
		return
	}

	io.WriteLine(fmt.Sprintf("%-20s %-8s %s", "SKILL", "USAGE", "DESCRIPTION"))
	io.WriteLine("----------------------------------------------------------")
	for _, s := range skills {
		usage := "0"
		if s.CallCount > 0 {
			usage = fmt.Sprintf("%d", s.CallCount)
		}
		io.WriteLine(fmt.Sprintf("%-20s %-8s %s", s.Name, usage, s.Description))
	}
	io.WriteLine("")
	io.WriteLine("Use search_skills to find skills, load_skill to load one.")
	io.WriteLine("")
}

func (c *Coordinator) handleCancelCmd(line string, io transport.UserIO) {
	parts := strings.Fields(line)
	if len(parts) > 1 {
		taskID := parts[1]
		if c.pool.Cancel(taskID) {
			io.WriteLine(fmt.Sprintf("Task %s cancelled.", taskID))
		} else {
			io.WriteLine(fmt.Sprintf("No running task found with ID: %s", taskID))
		}
	} else {
		c.pool.CancelAll()
		io.WriteLine("All running tasks cancelled.")
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
