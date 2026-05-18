package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"dolphin/internal/event"
	"dolphin/internal/mcp"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

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
	// Always-available tools for creating/updating skills and commands
	c.registerCoordTool("create_skill",
		"Create a new skill with the given name, description, and markdown content. Use this to teach the agent new capabilities that can be reused across sessions. If a skill with the same name already exists, it will be overwritten.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Unique skill name"},
				"description": map[string]any{"type": "string", "description": "Brief description of the skill's purpose"},
				"content":     map[string]any{"type": "string", "description": "Full markdown content with instructions, examples, and guidelines"},
			},
			"required": []string{"name", "description", "content"},
		},
		c.handleCreateSkill,
	)
	c.registerCoordTool("update_skill",
		"Update an existing skill's description and content. Use this to improve or correct a skill. If the skill does not exist, it will be created.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Skill name to update"},
				"description": map[string]any{"type": "string", "description": "Updated description"},
				"content":     map[string]any{"type": "string", "description": "Updated markdown content"},
			},
			"required": []string{"name", "description", "content"},
		},
		c.handleUpdateSkill,
	)
	c.registerCoordTool("create_command",
		"Create a new user-defined /command with the given name, description, and markdown content. The command will be invocable by typing /<name> in future conversations.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Command name (used as /<name>)"},
				"description": map[string]any{"type": "string", "description": "Brief description of what the command does"},
				"content":     map[string]any{"type": "string", "description": "Full markdown content with instructions sent to the LLM when /<name> is invoked"},
			},
			"required": []string{"name", "description", "content"},
		},
		c.handleCreateCommand,
	)
	c.registerCoordTool("update_command",
		"Update an existing /command's description and content. If the command does not exist, it will be created.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string", "description": "Command name to update"},
				"description": map[string]any{"type": "string", "description": "Updated description"},
				"content":     map[string]any{"type": "string", "description": "Updated markdown content"},
			},
			"required": []string{"name", "description", "content"},
		},
		c.handleUpdateCommand,
	)
	// Self-evolution only: destructive operations
	if c.cfg.Flags.SelfEvolution {
		c.registerCoordTool("delete_skill",
			"Permanently delete a skill by name. Use this to remove outdated or incorrect skills.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name to delete"},
				},
				"required": []string{"name"},
			},
			c.handleDeleteSkill,
		)
		c.registerCoordTool("delete_command",
			"Permanently delete a user-defined /command by name.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Command name to delete"},
				},
				"required": []string{"name"},
			},
			c.handleDeleteCommand,
		)
		c.registerCoordTool("reload",
			"Reload (restart) the agent. Disconnects the current session and triggers a clean restart. Config changes that require a restart take effect after this.",
			map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			c.handleReload,
		)
		c.registerCoordTool("session_dump",
			"Dump all events from a session by ID. Supports list format (default) or mermaid sequence diagram. Use /sessions to find session IDs.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "Session ID to dump"},
					"format": map[string]any{"type": "string", "description": "Output format: list (default) or mermaid", "enum": []string{"list", "mermaid"}},
				},
				"required": []string{"id"},
			},
			c.handleSessionDumpTool,
		)
	}
	c.registerCoordTool("context",
		"Show the full agent context including system prompt, available agents, tools, skills, pending results, and config settings. Use this to understand the current execution environment.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		c.handleContextTool,
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
	if c.cfg.Flags.SelfEvolution {
		c.registerCoordTool("config",
			"Read and modify runtime configuration. Actions: list (show all settings), get (read a path), set (modify a setting), save (persist to disk), delete (reset a setting to its default value). Changes to MCP tool settings (shell/cdp/email/webhook) take effect immediately. Changes to LLM settings take effect on the next conversation turn.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "get", "set", "save", "delete"},
						"description": "Action: list, get (read a path), set (modify a path), save (persist to disk), delete (reset to default)",
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
	} else {
		c.registerCoordTool("config",
			"Read runtime configuration. Actions: list (show all settings), get (read a path). To modify configuration, enable self_evolution (set flags.self_evolution = true in config.yaml).",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "get"},
						"description": "Action: list (show all settings), get (read a path)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Config path in dot notation, e.g. mcp.shell.timeout_seconds. Use list to see all paths.",
					},
				},
				"required": []string{"action"},
			},
			c.handleConfig,
		)
	}
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}

	task := Task{
		ID:      xid.New().String(),
		Input:   params.Task,
		Timeout: params.Timeout,
	}

	if err := c.pool.Dispatch(params.Agent, task); err != nil {
		return &mcp.ToolResult{Content: "dispatch failed: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
	fmt.Fprintf(&sb, "%d agent(s) available:\n", len(agents))
	for _, a := range agents {
		fmt.Fprintf(&sb, "- %s [%s] [%s] tasks: %d\n", a.Name, a.Status, a.Kind, a.TasksDone)
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) handleCancelTask(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}

	defs := c.toolReg.SearchTools(params.Query)
	if len(defs) == 0 {
		return &mcp.ToolResult{Content: fmt.Sprintf("No MCP tools found matching %q.", params.Query)}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d MCP tool(s) matching %q:\n", len(defs), params.Query)
	for _, d := range defs {
		stats := c.toolReg.ToolStats()
		usage := ""
		if s, ok := stats[d.Name]; ok && s.CallCount > 0 {
			usage = fmt.Sprintf(" (used %d times)", s.CallCount)
		}
		fmt.Fprintf(&sb, "- %s: %s%s\n", d.Name, d.Description, usage)
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

// autoLoadMCPTools pre-loads MCP tools that are enabled in config so the LLM
// doesn't waste turns on search_mcp_tools + load_mcp_tools discovery.
func (c *Coordinator) autoLoadMCPTools() {
	enabled := []struct {
		name    string
		enabled bool
	}{
		{"shell", c.cfg.MCP.Shell.Enabled},
		{"cdp", c.cfg.MCP.CDP.Enabled},
		{"email", c.cfg.MCP.Email.Enabled},
		{"webhook", c.cfg.MCP.Webhook.Enabled},
	}
	for _, t := range enabled {
		if t.enabled {
			if _, ok := c.toolReg.Get(t.name); ok {
				c.loadedTools[t.name] = true
			}
		}
	}
}

// handleLoadMCPTools loads MCP tools by name into the LLM's active tool set for the next turn.
func (c *Coordinator) handleLoadMCPTools(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		fmt.Fprintf(&sb, "Loaded %d tool(s): %s\n", len(loaded), strings.Join(loaded, ", "))
		sb.WriteString("They will be available in the tool list starting from your next turn.")
	}
	if len(notFound) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "Tool(s) not found: %s. Use search_mcp_tools to discover available tools.", strings.Join(notFound, ", "))
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}

	results := c.skills.Search(params.Query)
	if len(results) == 0 {
		return &mcp.ToolResult{Content: fmt.Sprintf("No skills found matching %q.", params.Query)}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d skill(s) matching %q:\n", len(results), params.Query)
	for _, s := range results {
		usage := ""
		if s.CallCount > 0 {
			usage = fmt.Sprintf(" (used %d times)", s.CallCount)
		}
		fmt.Fprintf(&sb, "- %s: %s%s\n", s.Name, s.Description, usage)
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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

func (c *Coordinator) handleCreateSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.skills == nil {
		return &mcp.ToolResult{Content: "Skills system is not available.", IsError: true}, nil
	}
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Skill name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Skill content is required.", IsError: true}, nil
	}
	if err := c.skills.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to create skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q created successfully.", params.Name)}, nil
}

func (c *Coordinator) handleUpdateSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.skills == nil {
		return &mcp.ToolResult{Content: "Skills system is not available.", IsError: true}, nil
	}
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Skill name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Skill content is required.", IsError: true}, nil
	}
	if err := c.skills.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to update skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q updated successfully.", params.Name)}, nil
}

func (c *Coordinator) handleDeleteSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.skills == nil {
		return &mcp.ToolResult{Content: "Skills system is not available.", IsError: true}, nil
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Skill name is required.", IsError: true}, nil
	}
	if _, ok := c.skills.Get(params.Name); !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q not found.", params.Name), IsError: true}, nil
	}
	if err := c.skills.Unregister(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to delete skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q deleted successfully.", params.Name)}, nil
}

// ---- Command handlers (LLM tools) ----

func (c *Coordinator) handleCreateCommand(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.commands == nil {
		return &mcp.ToolResult{Content: "Commands system is not available.", IsError: true}, nil
	}
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Command name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Command content is required.", IsError: true}, nil
	}
	if err := c.commands.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to create command %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Command /%s created successfully. Users can now run it by typing /%s.", params.Name, params.Name)}, nil
}

func (c *Coordinator) handleUpdateCommand(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.commands == nil {
		return &mcp.ToolResult{Content: "Commands system is not available.", IsError: true}, nil
	}
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Command name is required.", IsError: true}, nil
	}
	if params.Content == "" {
		return &mcp.ToolResult{Content: "Command content is required.", IsError: true}, nil
	}
	if err := c.commands.Register(params.Name, params.Description, params.Content); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to update command %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Command /%s updated successfully.", params.Name)}, nil
}

func (c *Coordinator) handleDeleteCommand(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if c.commands == nil {
		return &mcp.ToolResult{Content: "Commands system is not available.", IsError: true}, nil
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Command name is required.", IsError: true}, nil
	}
	if _, ok := c.commands.Get(params.Name); !ok {
		return &mcp.ToolResult{Content: fmt.Sprintf("Command /%s not found.", params.Name), IsError: true}, nil
	}
	if err := c.commands.Unregister(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to delete command %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Command /%s deleted successfully.", params.Name)}, nil
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	task := &scheduler.CronTask{
		Name:        params.Name,
		Schedule:    params.Schedule,
		Description: params.Description,
		Enabled:     true,
		Task:        params.Task,
	}
	if err := c.cronMgr.AddTask(task); err != nil {
		return &mcp.ToolResult{Content: "add cron task: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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
	fmt.Fprintf(&sb, "%d scheduled task(s):\n", len(tasks))
	for _, t := range tasks {
		status := "enabled"
		if !t.Enabled {
			status = "disabled"
		}
		fmt.Fprintf(&sb, "- %s [%s] %s (%s)\n", t.Name, status, t.Schedule, t.Description)
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
			fmt.Fprintf(&sb, "  %s %s: %s\n", mark, r.TaskName, msg)
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
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
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

func (c *Coordinator) handleReload(ctx context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	c.reloadRequested = true
	if c.events != nil && ctx != nil {
		c.events.Emit(ctx, event.Event{Type: event.TypeAgentReload})
	}
	zap.S().Infow("reload requested by LLM")
	return &mcp.ToolResult{
		Content: "Reloading agent. The current session will disconnect and the agent will restart cleanly.",
	}, nil
}

func (c *Coordinator) handleContextTool(_ context.Context, _ json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{
		Content: c.buildDynamicPrompt(),
	}, nil
}

func (c *Coordinator) handleSessionDumpTool(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		ID     string `json:"id"`
		Format string `json:"format,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}
	if params.Format == "" {
		params.Format = "list"
	}
	sessionPath := filepath.Join(c.sessMgr.Dir(), params.ID+".jsonl")
	events, err := session.ReadEvents(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}
	var sb strings.Builder
	switch params.Format {
	case "mermaid":
		c.dumpSessionMermaidTo(events, params.ID, &sb)
	default:
		c.dumpSessionListTo(events, params.ID, &sb)
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) dumpSessionListTo(events []session.SessionEvent, id string, sb *strings.Builder) {
	fmt.Fprintf(sb, "Session: %s (%d events)\n", id, len(events))
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
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			line += ": " + content
		}
		sb.WriteString(line + "\n")
	}
}

func (c *Coordinator) dumpSessionMermaidTo(events []session.SessionEvent, id string, sb *strings.Builder) {
	fmt.Fprintf(sb, "sequenceDiagram\n")
	fmt.Fprintf(sb, "    participant User as User\n")
	fmt.Fprintf(sb, "    participant Agent as Agent\n")
	toolNames := make(map[string]bool)
	for _, evt := range events {
		if evt.ToolName != "" {
			toolNames[evt.ToolName] = true
		}
	}
	for name := range toolNames {
		fmt.Fprintf(sb, "    participant %s as %s\n", strings.ReplaceAll(name, "-", "_"), name)
	}
	fmt.Fprintf(sb, "\n")
	var lastTurn int
	for _, evt := range events {
		if evt.Turn != lastTurn {
			fmt.Fprintf(sb, "    Note over User,Agent: Turn %d\n", evt.Turn)
			lastTurn = evt.Turn
		}
		switch evt.Role {
		case "user":
			content := string(evt.Content)
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			fmt.Fprintf(sb, "    User->>Agent: %s\n", content)
		case "assistant":
			if evt.ToolName != "" {
				toolName := strings.ReplaceAll(evt.ToolName, "-", "_")
				input := string(evt.ToolInput)
				if len(input) > 60 {
					input = input[:60] + "..."
				}
				fmt.Fprintf(sb, "    Agent->>+%s: %s\n", toolName, input)
			} else {
				content := string(evt.Content)
				if len(content) > 80 {
					content = content[:80] + "..."
				}
				fmt.Fprintf(sb, "    Agent-->>User: %s\n", content)
			}
		default:
			if evt.ToolName != "" && evt.Type == "tool_result" {
				toolName := strings.ReplaceAll(evt.ToolName, "-", "_")
				result := string(evt.Content)
				if len(result) > 80 {
					result = result[:80] + "..."
				}
				prefix := "-->>-"
				if evt.IsError {
					prefix = "-x->-"
				}
				fmt.Fprintf(sb, "    %s%sAgent: %s\n", toolName, prefix, result)
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
