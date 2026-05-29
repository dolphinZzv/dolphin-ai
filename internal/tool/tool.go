package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"dolphin/internal/command"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/skill"
	"dolphin/internal/types"
)

// Executor defines how a single tool source lists and executes tools.
type Executor interface {
	List(ctx context.Context) ([]types.ToolDef, error)
	Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error)
}

// BuiltinHandler is a function that implements a builtin tool.
type BuiltinHandler func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error)

// builtinTool describes a registered builtin tool.
type builtinTool struct {
	def     types.ToolDef
	handler BuiltinHandler
}

// Registry aggregates multiple tool sources including builtins and MCP clients.
type Registry struct {
	mu       sync.RWMutex
	builtins map[string]builtinTool
	sources  []Executor
}

func NewRegistry() *Registry {
	return &Registry{
		builtins: make(map[string]builtinTool),
	}
}

// RegisterBuiltin adds a builtin tool.
func (r *Registry) RegisterBuiltin(name, description string, schema json.RawMessage, handler BuiltinHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.builtins[name] = builtinTool{
		def: types.ToolDef{
			Name:        name,
			Description: description,
			Schema:      schema,
		},
		handler: handler,
	}
}

// AddSource adds a tool source (e.g. MCP client, skill tools).
func (r *Registry) AddSource(src Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sources = append(r.sources, src)
}

// List returns all tool definitions from all sources.
func (r *Registry) List(ctx context.Context) ([]types.ToolDef, error) {
	r.mu.RLock()
	sources := r.sources
	r.mu.RUnlock()

	// Collect builtin defs under a dedicated read lock.
	r.mu.RLock()
	all := make([]types.ToolDef, 0, len(r.builtins))
	for _, b := range r.builtins {
		all = append(all, b.def)
	}
	r.mu.RUnlock()

	for _, s := range sources {
		defs, err := s.List(ctx)
		if err != nil {
			continue
		}
		all = append(all, defs...)
	}
	return all, nil
}

// Execute finds and executes a tool by name.
func (r *Registry) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	// Check builtins first — O(1) map lookup.
	r.mu.RLock()
	b, ok := r.builtins[call.Name]
	sources := r.sources
	r.mu.RUnlock()

	if ok {
		var args json.RawMessage
		if call.Arguments != "" {
			args = json.RawMessage(call.Arguments)
		}
		return b.handler(ctx, args)
	}

	// Check external sources
	for _, s := range sources {
		defs, err := s.List(ctx)
		if err != nil {
			continue
		}
		for _, d := range defs {
			if d.Name == call.Name {
				return s.Execute(ctx, call)
			}
		}
	}

	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    fmt.Sprintf("tool %q not found", call.Name),
		IsError:    true,
	}, nil
}

// CatalogEntry describes an available MCP server.
type CatalogEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Tags        []string `json:"tags"`
}

// Catalog provides searchable MCP server directory.
type Catalog struct {
	entries []CatalogEntry
}

func NewCatalog(entries []CatalogEntry) *Catalog {
	return &Catalog{entries: entries}
}

func (c *Catalog) Search(query string) []CatalogEntry {
	q := strings.ToLower(query)
	var results []CatalogEntry
	for _, e := range c.entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			results = append(results, e)
		}
		for _, tag := range e.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				results = append(results, e)
				break
			}
		}
	}
	return results
}

// MetaEntry describes a builtin meta-tool with its schema.
type MetaEntry struct {
	Handler BuiltinHandler
	Schema  json.RawMessage
}

// MetaHandler returns builtin handlers and schemas for mcp_search and mcp_load.
func MetaHandler(catalog *Catalog, registry *Registry) map[string]MetaEntry {
	return map[string]MetaEntry{
		"mcp_search": {
			Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query for MCP servers"}},"required":["query"]}`),
			Handler: func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				var req struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
				}
				results := catalog.Search(req.Query)
				data, _ := json.Marshal(results)
				return &types.ToolResult{Content: string(data)}, nil
			},
		},
		"mcp_load": {
			Schema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL of the MCP server to load"}},"required":["url"]}`),
			Handler: func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				var req struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
				}
				client := mcp.NewClient(req.URL)
				defs, err := client.List(ctx)
				if err != nil {
					return &types.ToolResult{Content: "failed to connect: " + err.Error(), IsError: true}, nil
				}
				registry.AddSource(client)
				return &types.ToolResult{
					Content: fmt.Sprintf("loaded %d tools from %s", len(defs), req.URL),
				}, nil
			},
		},
	}
}

// ExecuteWithTimeout runs a tool execution with timeout.
func ExecuteWithTimeout(ctx context.Context, reg *Registry, call types.ToolCall, timeout time.Duration) (*types.ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct {
		result *types.ToolResult
		err    error
	}, 1)

	go func() {
		result, err := reg.Execute(ctx, call)
		done <- struct {
			result *types.ToolResult
			err    error
		}{result, err}
	}()

	select {
	case r := <-done:
		return r.result, r.err
	case <-ctx.Done():
		return &types.ToolResult{
			ToolCallID: call.ID,
			Content:    "tool execution timed out",
			IsError:    true,
		}, nil
	}
}

// SkillStore is the subset of skill.Store that RegisterSkillTools needs.
type SkillStore interface {
	List(ctx context.Context) ([]skill.Skill, error)
	Get(ctx context.Context, name string) (*skill.Skill, error)
	Save(ctx context.Context, sk skill.Skill) error
	Delete(ctx context.Context, name string) error
	Search(ctx context.Context, query string) ([]skill.Skill, error)
}

// SkillAdapter wraps a skill.Store to satisfy SkillStore interface.
type SkillAdapter struct {
	Store skill.Store
}

func (a SkillAdapter) List(ctx context.Context) ([]skill.Skill, error) { return a.Store.List(ctx) }
func (a SkillAdapter) Get(ctx context.Context, name string) (*skill.Skill, error) {
	return a.Store.Get(ctx, name)
}
func (a SkillAdapter) Save(ctx context.Context, sk skill.Skill) error { return a.Store.Save(ctx, sk) }
func (a SkillAdapter) Delete(ctx context.Context, name string) error {
	return a.Store.Delete(ctx, name)
}
func (a SkillAdapter) Search(ctx context.Context, query string) ([]skill.Skill, error) {
	return a.Store.Search(ctx, query)
}

// RegisterSkillTools registers builtin tools for skill CRUD.
func RegisterSkillTools(r *Registry, store SkillStore, cmdReg *command.Registry) {
	skillSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"prompt":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"enabled":{"type":"boolean"}},"required":["name"]}`)
	nameSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	querySchema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)

	r.RegisterBuiltin("skill_new", "Create a new skill. Args: {name, description?, prompt?, tools?}", skillSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var sk skill.Skill
		if err := json.Unmarshal(args, &sk); err != nil {
			return &types.ToolResult{Content: "invalid skill definition: " + err.Error(), IsError: true}, nil
		}
		if sk.Name == "" {
			return &types.ToolResult{Content: "skill name is required", IsError: true}, nil
		}
		if err := store.Save(ctx, sk); err != nil {
			return &types.ToolResult{Content: "failed to save skill: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "skill '" + sk.Name + "' created"}, nil
	})

	r.RegisterBuiltin("skill_search", "Search for skills by query", querySchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args", IsError: true}, nil
		}
		results, _ := store.Search(ctx, req.Query)
		data, _ := json.Marshal(results)
		return &types.ToolResult{Content: string(data)}, nil
	})

	r.RegisterBuiltin("skill_load", "Load/enable a skill by name", nameSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args", IsError: true}, nil
		}
		sk, err := store.Get(ctx, req.Name)
		if err != nil {
			return &types.ToolResult{Content: "skill not found: " + req.Name, IsError: true}, nil
		}
		sk.Enabled = true
		store.Save(ctx, *sk)
		if cmdReg != nil && len(sk.Commands) > 0 {
			cmdReg.RegisterFromSkill(sk.Name, sk.Commands)
		}
		return &types.ToolResult{Content: "skill '" + sk.Name + "' loaded"}, nil
	})

	r.RegisterBuiltin("skill_update", "Update an existing skill. Args: {name, description?, prompt?, tools?, enabled?}", skillSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var sk skill.Skill
		if err := json.Unmarshal(args, &sk); err != nil {
			return &types.ToolResult{Content: "invalid skill definition", IsError: true}, nil
		}
		if sk.Name == "" {
			return &types.ToolResult{Content: "skill name is required", IsError: true}, nil
		}
		if err := store.Save(ctx, sk); err != nil {
			return &types.ToolResult{Content: "failed to update: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "skill '" + sk.Name + "' updated"}, nil
	})

	r.RegisterBuiltin("skill_delete", "Delete a skill by name", nameSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args", IsError: true}, nil
		}
		sk, getErr := store.Get(ctx, req.Name)
		if err := store.Delete(ctx, req.Name); err != nil {
			return &types.ToolResult{Content: "failed to delete: " + err.Error(), IsError: true}, nil
		}
		if cmdReg != nil && getErr == nil && len(sk.Commands) > 0 {
			cmdReg.UnregisterFromSkill(sk.Name, sk.Commands)
		}
		return &types.ToolResult{Content: "skill '" + req.Name + "' deleted"}, nil
	})
}

// RegisterCommandTools registers builtin tools for LLM-managed command CRUD.
func RegisterCommandTools(r *Registry, cmdReg *command.Registry) {
	cmdSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Command name invoked via /name in REPL"},"description":{"type":"string","description":"Short description"},"prompt":{"type":"string","description":"System prompt for the command handler"}},"required":["name","prompt"]}`)
	cmdNameSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)

	r.RegisterBuiltin("command_register", "Register a new CLI command handled by the LLM. Args: {name, description?, prompt}", cmdSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Prompt      string `json:"prompt"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args: " + err.Error(), IsError: true}, nil
		}
		if req.Name == "" || req.Prompt == "" {
			return &types.ToolResult{Content: "name and prompt are required", IsError: true}, nil
		}
		if err := cmdReg.RegisterCommandTool(req.Name, req.Description, req.Prompt); err != nil {
			return &types.ToolResult{Content: "failed to register command: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "command '" + req.Name + "' registered"}, nil
	})

	r.RegisterBuiltin("command_unregister", "Unregister a previously registered command. Args: {name}", cmdNameSchema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args", IsError: true}, nil
		}
		if err := cmdReg.UnregisterCommandTool(req.Name); err != nil {
			return &types.ToolResult{Content: "failed to unregister: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "command '" + req.Name + "' unregistered"}, nil
	})
}

// RegisterSessionTools registers builtin tools for LLM-managed session operations.
func RegisterSessionTools(r *Registry, mgr *session.Manager) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Session ID"}},"required":["id"]}`)

	r.RegisterBuiltin("session_list", "List all sessions with their IDs and timestamps", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		sessions, err := mgr.List(ctx)
		if err != nil {
			return &types.ToolResult{Content: "failed to list sessions: " + err.Error(), IsError: true}, nil
		}
		if len(sessions) == 0 {
			return &types.ToolResult{Content: "no sessions found"}, nil
		}
		var sb strings.Builder
		for _, s := range sessions {
			title := s.Title
			if title == "" {
				title = "(untitled)"
			}
			active := ""
			if s.Active {
				active = " [active]"
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (created: %s)%s\n", s.ID, title, s.CreatedAt.Format("2006-01-02 15:04:05"), active))
		}
		return &types.ToolResult{Content: sb.String()}, nil
	})

	r.RegisterBuiltin("session_switch", "Switch to a different session by ID. Args: {id}", schema, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return &types.ToolResult{Content: "invalid args", IsError: true}, nil
		}
		if req.ID == "" {
			return &types.ToolResult{Content: "session ID is required", IsError: true}, nil
		}
		s, err := mgr.SwitchTo(ctx, req.ID)
		if err != nil {
			return &types.ToolResult{Content: "failed to switch: " + err.Error(), IsError: true}, nil
		}
		return &types.ToolResult{Content: "switched to session " + s.ID}, nil
	})
}
