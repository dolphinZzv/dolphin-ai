package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/mcp"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/subsystem"

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
	if c.agent.cfg.Flags.SelfEvolution {
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
		// Self-evolution MCP server management tools
		c.registerCoordTool("install_mcp_server",
			"Install an MCP server from a configured MCP repo. Fetches the repo manifest, finds the matching server by name, and adds it to the config file.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "MCP server name to install"},
				},
				"required": []string{"name"},
			},
			c.handleInstallMCPServer,
		)
		c.registerCoordTool("uninstall_mcp_server",
			"Permanently remove an MCP server from the config file.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "MCP server name to uninstall"},
				},
				"required": []string{"name"},
			},
			c.handleUninstallMCPServer,
		)
		c.registerCoordTool("enable_mcp_server",
			"Enable a disabled MCP server in the config file.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "MCP server name to enable"},
				},
				"required": []string{"name"},
			},
			c.handleEnableMCPServer,
		)
		c.registerCoordTool("disable_mcp_server",
			"Disable an MCP server without removing its config. The server can be re-enabled later.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "MCP server name to disable"},
				},
				"required": []string{"name"},
			},
			c.handleDisableMCPServer,
		)
		c.registerCoordTool("disable_skill",
			"Disable a skill by removing it from memory and renaming its directory to .disabled/. The skill can be re-enabled later using enable_skill.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name to disable"},
				},
				"required": []string{"name"},
			},
			c.handleDisableSkill,
		)
		c.registerCoordTool("enable_skill",
			"Enable a previously disabled skill. Restores the skill directory from .disabled/ and reloads it into memory.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name to enable"},
				},
				"required": []string{"name"},
			},
			c.handleEnableSkill,
		)
		c.registerCoordTool("uninstall_skill",
			"Permanently delete a skill and all its files. This cannot be undone — use disable_skill instead to preserve the files.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Skill name to uninstall"},
				},
				"required": []string{"name"},
			},
			c.handleUninstallSkill,
		)
		c.registerCoordTool("install_agent",
			"Install an agent from a configured repo to the local agents directory. Fetches agents.json from repos, finds the matching agent, and creates the agent definition locally.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Agent name to install"},
				},
				"required": []string{"name"},
			},
			c.handleInstallAgent,
		)
		c.registerCoordTool("search_agents",
			"Search available agent definitions from remote repos by name or description. Agents are reusable worker configurations with specific roles and tool sets.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query (matched against agent name and description)"},
				},
				"required": []string{"query"},
			},
			c.handleSearchAgents,
		)
		c.registerCoordTool("disable_agent",
			"Disable a persistent agent by renaming its directory to .disabled/. The agent is removed from the pool but can be re-enabled later using enable_agent.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Agent name to disable"},
				},
				"required": []string{"name"},
			},
			c.handleDisableAgent,
		)
		c.registerCoordTool("enable_agent",
			"Enable a previously disabled persistent agent. Restores the agent directory from .disabled/ and re-adds it to the pool.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Agent name to enable"},
				},
				"required": []string{"name"},
			},
			c.handleEnableAgent,
		)
		c.registerCoordTool("uninstall_agent",
			"Permanently delete a persistent agent and all its files. This cannot be undone — use disable_agent instead to preserve the files.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Agent name to uninstall"},
				},
				"required": []string{"name"},
			},
			c.handleUninstallAgent,
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
	if c.agent.cfg.Flags.SelfEvolution {
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
	// Register subsystem tools
	for _, td := range subsystem.ToolDefs() {
		if td.SelfEvolution && !c.agent.cfg.Flags.SelfEvolution {
			continue
		}
		c.registerCoordTool(td.Name, td.Description, td.Schema, td.Handler)
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
	c.agent.toolReg.Register(&handlerTool{
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

	if c.agent.events != nil {
		c.agent.events.Emit(ctx, event.Event{
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

	workspace := TempAgentWorkspace(&c.agent.cfg.Pool, params.Name)
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = c.agent.cfg.Pool.DefaultTimeout
	}

	def := &AgentDef{
		Name:      params.Name,
		Role:      params.Role,
		Tools:     params.Tools,
		Model:     params.Model,
		Workspace: workspace,
		Timeout:   timeout,
	}

	c.pool.Add(params.Name, def, AgentCoord, c.agent, c.agent.toolReg)

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

	defs := c.agent.toolReg.SearchTools(params.Query)
	if len(defs) == 0 {
		return &mcp.ToolResult{Content: fmt.Sprintf("No MCP tools found matching %q.", params.Query)}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d MCP tool(s) matching %q:\n", len(defs), params.Query)
	for _, d := range defs {
		stats := c.agent.toolReg.ToolStats()
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
		{"shell", c.agent.cfg.MCP.Shell.Enabled},
		{"cdp", c.agent.cfg.MCP.CDP.Enabled},
		{"email", c.agent.cfg.MCP.Email.Enabled},
		{"webhook", c.agent.cfg.MCP.Webhook.Enabled},
		{"web_search", c.agent.cfg.MCP.WebSearch.Enabled},
	}
	for _, t := range enabled {
		if t.enabled {
			if _, ok := c.agent.toolReg.Get(t.name); ok {
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
		if _, ok := c.agent.toolReg.Get(name); ok {
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

	if c.agent.events != nil {
		c.agent.events.Emit(ctx, event.Event{
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
	if c.agent.events != nil && ctx != nil {
		c.agent.events.Emit(ctx, event.Event{Type: event.TypeAgentReload})
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
	sessionPath := filepath.Join(c.agent.sessMgr.Dir(), params.ID+".jsonl")
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

// ---- Self-evolution MCP server tool handlers ----

func (c *Coordinator) handleInstallMCPServer(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "MCP server name is required.", IsError: true}, nil
	}

	// Check if already installed
	if _, exists := c.agent.cfg.MCP.Servers[params.Name]; exists {
		return &mcp.ToolResult{Content: fmt.Sprintf("MCP server %q is already installed.", params.Name)}, nil
	}

	// Fetch from repos
	if len(c.agent.cfg.MCP.Repos) == 0 {
		return &mcp.ToolResult{Content: "No MCP repos configured. Add repos to mcp.repos in config.yaml.", IsError: true}, nil
	}
	if err := c.installMCPServerFromRepos(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to install MCP server: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("MCP server %q installed successfully. Restart to load it.", params.Name)}, nil
}

func (c *Coordinator) installMCPServerFromRepos(name string) error {
	fetcher := config.NewRepoFetcher(c.getCacheDir())
	if ex, err := os.Executable(); err == nil {
		fetcher.SetLocalDir(filepath.Dir(ex))
	}

	ctx, cancel := context.WithTimeout(context.Background(), coordTimeout(c.agent.cfg))
	manifests := fetcher.FetchAll(ctx, c.agent.cfg.MCP.Repos)
	cancel()

	var found *config.ToolEntry
	for _, m := range manifests {
		for _, t := range m.Tools {
			if t.Name == name {
				found = &t
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return fmt.Errorf("MCP server %q not found in any configured repo", name)
	}
	return config.ApplyTools(nil, []config.ToolEntry{*found})
}

func (c *Coordinator) getCacheDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, config.UserConfigDir, "cache")
}

func (c *Coordinator) handleUninstallMCPServer(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "MCP server name is required.", IsError: true}, nil
	}

	if err := config.RemoveMCPServer(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to uninstall MCP server: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("MCP server %q uninstalled. Restart to apply changes.", params.Name)}, nil
}

func (c *Coordinator) handleEnableMCPServer(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "MCP server name is required.", IsError: true}, nil
	}

	if err := config.ToggleMCPServer(params.Name, true); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to enable MCP server: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("MCP server %q enabled. Restart to apply changes.", params.Name)}, nil
}

func (c *Coordinator) handleDisableMCPServer(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "MCP server name is required.", IsError: true}, nil
	}

	if err := config.ToggleMCPServer(params.Name, false); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to disable MCP server: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("MCP server %q disabled. Restart to apply changes.", params.Name)}, nil
}

// ---- Self-evolution Skill tool handlers ----

func (c *Coordinator) handleDisableSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
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
	if err := c.skills.Disable(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to disable skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q disabled. Use enable_skill to re-enable it.", params.Name)}, nil
}

func (c *Coordinator) handleEnableSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
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
	if err := c.skills.Enable(params.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to enable skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q enabled and loaded.", params.Name)}, nil
}

func (c *Coordinator) handleUninstallSkill(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
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
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to uninstall skill %q: %v", params.Name, err), IsError: true}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Skill %q permanently uninstalled.", params.Name)}, nil
}

// ---- Agent tool handlers ----

func (c *Coordinator) handleInstallAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Agent name is required.", IsError: true}, nil
	}

	// Check if already installed locally
	agentsDir := filepath.Join(config.ProjectConfigDir, "agents")
	if _, err := os.Stat(filepath.Join(agentsDir, params.Name, "agent.yaml")); err == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q is already installed locally.", params.Name)}, nil
	}

	// Check if disabled
	if _, err := os.Stat(filepath.Join(agentsDir, params.Name+".disabled", "agent.yaml")); err == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q is installed but disabled. Use enable_agent to restore it.", params.Name)}, nil
	}

	// Fetch agents.json from repos
	if len(c.agent.cfg.Skills.Repos) == 0 {
		return &mcp.ToolResult{Content: "No repos configured. Add repos to skills.repos in config.yaml.", IsError: true}, nil
	}

	fetcher := config.NewRepoFetcher(c.getCacheDir())
	if ex, err := os.Executable(); err == nil {
		fetcher.SetLocalDir(filepath.Dir(ex))
	}

	ctx, cancel := context.WithTimeout(context.Background(), coordTimeout(c.agent.cfg))
	manifests := fetcher.FetchAllAgentManifests(ctx, c.agent.cfg.Skills.Repos)
	cancel()

	var found *config.AgentManifestEntry
	for _, m := range manifests {
		for _, a := range m.Agents {
			if a.Name == params.Name {
				found = &a
				break
			}
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q not found in any configured repo.", params.Name), IsError: true}, nil
	}

	// Download agent repo from URL (or copy local path)
	agentDir := filepath.Join(agentsDir, params.Name)
	if err := downloadAgentRepo(found.URL, agentDir, found.Path); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to download agent repo: %v", err), IsError: true}, nil
	}

	// Verify agent.yaml exists
	agentYAML := filepath.Join(agentDir, "agent.yaml")
	if _, err := os.Stat(agentYAML); os.IsNotExist(err) {
		os.RemoveAll(agentDir)
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent repo does not contain agent.yaml."), IsError: true}, nil
	}

	// Load and add to pool
	loadedDef, err := loadAgentYAML(agentYAML)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q installed but failed to load agent.yaml: %v", params.Name, err), IsError: true}, nil
	}
	loadedDef.Name = params.Name
	if loadedDef.Workspace == "" {
		loadedDef.Workspace = filepath.Join(c.agent.cfg.Pool.WorkspaceDir, params.Name)
	}
	os.MkdirAll(loadedDef.Workspace, 0700)

	c.pool.Add(params.Name, loadedDef, AgentUser, c.agent, c.agent.toolReg)
	zap.S().Infow("installed agent from repo", "name", params.Name, "url", found.URL)
	return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q installed successfully from %s.", params.Name, found.URL)}, nil
}
func (c *Coordinator) handleSearchAgents(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}

	// Collect results: local agents + remote
	var results []string

	// Local agents from pool
	agents := c.pool.List()
	for _, a := range agents {
		if params.Query == "" || strings.Contains(strings.ToLower(a.Name), strings.ToLower(params.Query)) ||
			strings.Contains(strings.ToLower(a.Role), strings.ToLower(params.Query)) {
			results = append(results, fmt.Sprintf("- %s [%s] %s (tasks: %d)", a.Name, a.Kind, a.Role, a.TasksDone))
		}
	}

	// Remote: fetch agents.json from repos
	if len(c.agent.cfg.Skills.Repos) > 0 {
		fetcher := config.NewRepoFetcher(c.getCacheDir())
		if ex, err := os.Executable(); err == nil {
			fetcher.SetLocalDir(filepath.Dir(ex))
		}
		ctx, cancel := context.WithTimeout(context.Background(), coordTimeout(c.agent.cfg))
		manifests := fetcher.FetchAllAgentManifests(ctx, c.agent.cfg.Skills.Repos)
		cancel()
		for _, m := range manifests {
			for _, a := range m.Agents {
				if params.Query != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(params.Query)) &&
					!strings.Contains(strings.ToLower(a.Description), strings.ToLower(params.Query)) {
					continue
				}
				desc := a.Description
				if desc == "" {
					desc = a.Role
				}
				results = append(results, fmt.Sprintf("- %s [remote/%s] %s", a.Name, m.Name, desc))
			}
		}
	}

	if len(results) == 0 {
		if params.Query != "" {
			return &mcp.ToolResult{Content: fmt.Sprintf("No agents found matching %q.", params.Query)}, nil
		}
		return &mcp.ToolResult{Content: "No agents available."}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d agent(s):\n", len(results))
	for _, r := range results {
		sb.WriteString(r + "\n")
	}
	return &mcp.ToolResult{Content: sb.String()}, nil
}

func (c *Coordinator) agentsDir() string {
	return filepath.Join(config.ProjectConfigDir, "agents")
}

func (c *Coordinator) handleDisableAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Agent name is required.", IsError: true}, nil
	}

	agentDir := filepath.Join(c.agentsDir(), params.Name)
	disabledDir := filepath.Join(c.agentsDir(), params.Name+".disabled")

	// Check if agent exists and is not already disabled
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		// Maybe it is already disabled
		if _, err2 := os.Stat(disabledDir); err2 == nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q is already disabled. Use enable_agent to restore it.", params.Name)}, nil
		}
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q not found.", params.Name), IsError: true}, nil
	}

	// Remove from runtime pool first
	c.pool.Remove(params.Name)

	// Rename directory to .disabled/
	if err := os.Rename(agentDir, disabledDir); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to disable agent %q: %v", params.Name, err), IsError: true}, nil
	}

	zap.S().Infow("disabled agent", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q disabled. Use enable_agent to re-enable it.", params.Name)}, nil
}

func (c *Coordinator) handleEnableAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Agent name is required.", IsError: true}, nil
	}

	agentDir := filepath.Join(c.agentsDir(), params.Name)
	disabledDir := filepath.Join(c.agentsDir(), params.Name+".disabled")

	// Check disabled dir exists
	if _, err := os.Stat(disabledDir); os.IsNotExist(err) {
		if _, err2 := os.Stat(agentDir); err2 == nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q is already enabled.", params.Name)}, nil
		}
		return &mcp.ToolResult{Content: fmt.Sprintf("Disabled agent %q not found.", params.Name), IsError: true}, nil
	}

	// Rename back
	if err := os.Rename(disabledDir, agentDir); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to enable agent %q: %v", params.Name, err), IsError: true}, nil
	}

	// Re-load definition and add to pool
	def, err := loadAgentYAML(filepath.Join(agentDir, "agent.yaml"))
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q restored but failed to load definition: %v", params.Name, err), IsError: true}, nil
	}
	def.Name = params.Name
	if def.Workspace == "" {
		def.Workspace = filepath.Join(c.agent.cfg.Pool.WorkspaceDir, params.Name)
	}
	os.MkdirAll(def.Workspace, 0700)

	c.pool.Add(params.Name, def, AgentUser, c.agent, c.agent.toolReg)
	zap.S().Infow("enabled agent", "name", params.Name, "tools", def.Tools)
	return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q enabled and added to the pool.", params.Name)}, nil
}

func (c *Coordinator) handleUninstallAgent(_ context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}
	if params.Name == "" {
		return &mcp.ToolResult{Content: "Agent name is required.", IsError: true}, nil
	}

	// Try regular dir first, then disabled dir
	agentDir := filepath.Join(c.agentsDir(), params.Name)
	disabledDir := filepath.Join(c.agentsDir(), params.Name+".disabled")

	var targetDir string
	switch {
	case dirExists(agentDir):
		targetDir = agentDir
	case dirExists(disabledDir):
		targetDir = disabledDir
	default:
		return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q not found.", params.Name), IsError: true}, nil
	}

	// Remove from pool if running
	c.pool.Remove(params.Name)

	// Permanently delete
	if err := os.RemoveAll(targetDir); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to uninstall agent %q: %v", params.Name, err), IsError: true}, nil
	}

	zap.S().Infow("uninstalled agent", "name", params.Name)
	return &mcp.ToolResult{Content: fmt.Sprintf("Agent %q permanently uninstalled.", params.Name)}, nil
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// downloadAgentRepo downloads or copies an agent repo to the target directory.
// url can be:
//   - "owner/repo" (GitHub repo, downloaded as ZIP and extracted)
//   - a local path (copied directly)
//
// subPath is an optional subdirectory within the repo to extract.
func downloadAgentRepo(url, destDir, subPath string) error {
	if url == "" {
		return fmt.Errorf("no URL specified for agent repo")
	}

	// Local path
	if strings.HasPrefix(url, ".") || strings.HasPrefix(url, "/") || strings.HasPrefix(url, "~") {
		return copyAgentDir(url, destDir)
	}

	// SSH git URL (e.g. git@github.com:owner/repo.git)
	if strings.HasPrefix(url, "git@") {
		return gitCloneRepo(url, destDir)
	}

	// GitHub owner/repo format
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 && !strings.Contains(url, "://") && !strings.Contains(url, "\\") {
		return downloadGitHubRepo(url, destDir, subPath)
	}

	// Full URL
	return downloadGitHubRepo(url, destDir, subPath)
}

// gitCloneRepo clones a git repository using SSH URL.
func gitCloneRepo(repoURL, destDir string) error {
	cmd := exec.Command("git", "clone", repoURL, destDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return nil
}

// downloadGitHubRepo downloads a GitHub repo archive and extracts it.
// subPath is an optional subdirectory within the repo to extract.
func downloadGitHubRepo(repo, destDir, subPath string) error {
	// Handle both "owner/repo" and full URL formats
	if !strings.Contains(repo, "://") {
		repo = fmt.Sprintf("https://github.com/%s/archive/main.zip", repo)
	}

	client := &http.Client{Timeout: coordHTTPTimeout(nil)}
	resp, err := client.Get(repo)
	if err != nil {
		return fmt.Errorf("download repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download repo: HTTP %d", resp.StatusCode)
	}

	// Read the ZIP into memory
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	// Find the root directory name in the zip (GitHub zips have a top-level dir)
	var rootPrefix string
	for _, f := range reader.File {
		if !f.FileInfo().IsDir() {
			parts := strings.SplitN(f.Name, "/", 2)
			if len(parts) == 2 {
				rootPrefix = parts[0] + "/"
			}
			break
		}
	}

	// Extract all files, stripping the root directory
	for _, f := range reader.File {
		var name string
		if rootPrefix != "" && strings.HasPrefix(f.Name, rootPrefix) {
			name = strings.TrimPrefix(f.Name, rootPrefix)
		} else {
			name = f.Name
		}
		if name == "" {
			continue
		}
		// Filter by subpath if specified
		if subPath != "" {
			if strings.HasPrefix(name, subPath+"/") {
				name = strings.TrimPrefix(name, subPath+"/")
			} else if name == subPath {
				// If the subpath is a file, include it at root
				continue
			} else {
				continue
			}
		}
		if name == "" {
			continue
		}

		outPath := filepath.Join(destDir, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(outPath, 0700)
			continue
		}

		os.MkdirAll(filepath.Dir(outPath), 0700)
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open file in zip: %w", err)
		}

		out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create file: %w", err)
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return fmt.Errorf("extract file: %w", err)
		}
	}

	return nil
}

// copyAgentDir copies a local agent repo directory to the destination.
func copyAgentDir(src, destDir string) error {
	// Resolve ~ to home directory
	if strings.HasPrefix(src, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		src = filepath.Join(home, src[1:])
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("source path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory: %s", src)
	}

	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		outPath := filepath.Join(destDir, rel)
		if fi.IsDir() {
			return os.MkdirAll(outPath, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0600)
	})
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

// coordHTTPTimeout returns an HTTP client timeout from config.
// Uses Update.TimeoutSeconds if set, otherwise defaults to 30s.
func coordHTTPTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Update.TimeoutSeconds > 0 {
		return time.Duration(cfg.Update.TimeoutSeconds) * time.Second
	}
	return 30 * time.Second
}
