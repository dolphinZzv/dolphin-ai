package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphinzZ/internal/config"
	"dolphinzZ/internal/metrics"
)

// ToolDefinition is the public description of a tool.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Priority    int             `json:"priority"` // lower = preferred in tool listing; 0 = default (100)
	Source      string          `json:"source"`   // origin: "built-in", server name, etc.
}

// ToolCall is a request to execute a tool.
type ToolCall struct {
	Name      string
	Arguments json.RawMessage
}

// ToolResult is the result of a tool execution.
type ToolResult struct {
	Content string
	IsError bool
}

// Tool is the interface all MCP tools must implement.
type Tool interface {
	Definition() ToolDefinition
	Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error)
}

// ToolStats tracks usage statistics for a tool.
type ToolStats struct {
	CallCount     int64         `json:"call_count"`
	ErrorCount    int64         `json:"error_count"`
	LastCalledAt  time.Time     `json:"last_called_at"`
	TotalDuration time.Duration `json:"total_duration"`
}

// AverageDurationMs returns the average execution duration in milliseconds.
func (s *ToolStats) AverageDurationMs() float64 {
	if s.CallCount == 0 {
		return 0
	}
	return float64(s.TotalDuration.Milliseconds()) / float64(s.CallCount)
}

// DefaultPriority is the priority assigned to tools that don't set one.
const DefaultPriority = 100

// Registry manages all registered MCP tools, including external server tools.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	order   []string // registration order, used as tiebreaker in MostUsedTools
	servers []*ServerClient
	cfg     *config.MCPConfig
	filter  map[string]bool // nil = no filter; non-nil = only allow listed tools
	stats   map[string]*ToolStats

	// metrics collectors (lazily initialized)
	toolCalls    *metrics.Counter
	toolErrors   *metrics.Counter
	toolDuration *metrics.Histogram
}

func NewRegistry(cfg *config.Config) *Registry {
	return &Registry{
		tools:   make(map[string]Tool),
		servers: make([]*ServerClient, 0),
		cfg:     &cfg.MCP,
		stats:   make(map[string]*ToolStats),
	}
}

func (r *Registry) Register(t Tool) {
	def := t.Definition()
	r.mu.Lock()
	if _, exists := r.tools[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.tools[def.Name] = t
	if _, ok := r.stats[def.Name]; !ok {
		r.stats[def.Name] = &ToolStats{}
	}
	r.mu.Unlock()
}

// LoadServers starts external MCP servers defined in config and registers their tools.
func (r *Registry) LoadServers() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, cfg := range r.cfg.Servers {
		client, err := NewServerClient(name, cfg)
		if err != nil {
			return fmt.Errorf("mcp server %q: %w", name, err)
		}

		defs, err := client.ListTools()
		if err != nil {
			client.Close()
			return fmt.Errorf("list tools from %q: %w", name, err)
		}

		for _, def := range defs {
			def.Source = name
			wrapper := &serverTool{
				server: client,
				def:    def,
			}
			// Always register with server:name prefix for disambiguation
			r.tools[name+":"+def.Name] = wrapper
			r.stats[name+":"+def.Name] = &ToolStats{}
			slog.Debug("mcp tool registered", "tool", name+":"+def.Name, "server", name)
			// Also register with bare name if no collision
			if _, exists := r.tools[def.Name]; !exists {
				r.tools[def.Name] = wrapper
				r.stats[def.Name] = &ToolStats{}
				slog.Debug("mcp tool registered (bare)", "tool", def.Name, "server", name)
			}
		}

		r.servers = append(r.servers, client)
	}

	return nil
}

// CloseServers shuts down all external MCP servers.
func (r *Registry) CloseServers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.servers {
		s.Close()
	}
	r.servers = nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.filter != nil && !r.filter[name] {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

// ToolStats returns the usage statistics for all tools (snapshot).
func (r *Registry) ToolStats() map[string]ToolStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[string]ToolStats, len(r.stats))
	for name, s := range r.stats {
		m[name] = *s
	}
	return m
}

// MostUsedTools returns the top n most-used tools by call count.
func (r *Registry) MostUsedTools(n int) []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type entry struct {
		def ToolDefinition
		cnt int64
	}
	var list []entry
	for name, t := range r.tools {
		def := t.Definition()
		if r.filter != nil && !r.filter[name] {
			continue
		}
		cnt := int64(0)
		if s, ok := r.stats[name]; ok {
			cnt = s.CallCount
		}
		list = append(list, entry{def, cnt})
	}

	sort.Slice(list, func(i, j int) bool {
		pi := toolPriority(list[i].def)
		pj := toolPriority(list[j].def)
		if pi != pj {
			return pi < pj
		}
		if list[i].cnt != list[j].cnt {
			return list[i].cnt > list[j].cnt
		}
		// Tiebreaker: registration order
		return r.orderIndex(list[i].def.Name) < r.orderIndex(list[j].def.Name)
	})

	if n > len(list) {
		n = len(list)
	}
	defs := make([]ToolDefinition, n)
	for i := 0; i < n; i++ {
		defs[i] = list[i].def
	}
	return defs
}

// SearchTools returns tool definitions whose name or description matches the query.
func (r *Registry) SearchTools(query string) []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	q := strings.ToLower(query)
	var defs []ToolDefinition
	for name, t := range r.tools {
		def := t.Definition()
		if r.filter != nil && !r.filter[name] {
			continue
		}
		if strings.Contains(strings.ToLower(def.Name), q) ||
			strings.Contains(strings.ToLower(def.Description), q) {
			defs = append(defs, def)
		}
	}
	return defs
}

// FilteredView returns a Registry view restricted to the named tools.
// If names is empty, all tools are visible (no filter).
func (r *Registry) FilteredView(names []string) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make(map[string]Tool, len(r.tools))
	for name, tool := range r.tools {
		if len(names) > 0 {
			allowed := false
			for _, n := range names {
				if name == n {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		tools[name] = tool
	}

	servers := make([]*ServerClient, len(r.servers))
	copy(servers, r.servers)

	stats := make(map[string]*ToolStats, len(r.stats))
	for k, v := range r.stats {
		s := *v
		stats[k] = &s
	}

	var filter map[string]bool
	if len(names) > 0 {
		filter = make(map[string]bool, len(names))
		for _, n := range names {
			filter[n] = true
		}
	}

	return &Registry{
		tools:   tools,
		order:   r.order,
		servers: servers,
		cfg:     r.cfg,
		filter:  filter,
		stats:   stats,
	}
}

// toolPriority returns the effective priority of a tool definition.
// A value of 0 means the tool didn't set one, so use DefaultPriority.
func toolPriority(def ToolDefinition) int {
	if def.Priority <= 0 {
		return DefaultPriority
	}
	return def.Priority
}

// orderIndex returns the position of a tool in the registration order.
// Unknown tools get a large index so they sort last.
func (r *Registry) orderIndex(name string) int {
	for i, n := range r.order {
		if n == name {
			return i
		}
	}
	return len(r.order)
}

// Clone returns an independent copy of the registry with the same tools, order, and servers.
// Useful for per-connection registries that need to add local tools without
// affecting the shared registry.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make(map[string]Tool, len(r.tools))
	for k, v := range r.tools {
		tools[k] = v
	}

	servers := make([]*ServerClient, len(r.servers))
	copy(servers, r.servers)

	var filter map[string]bool
	if r.filter != nil {
		filter = make(map[string]bool, len(r.filter))
		for k, v := range r.filter {
			filter[k] = v
		}
	}

	stats := make(map[string]*ToolStats, len(r.stats))
	for k, v := range r.stats {
		s := *v
		stats[k] = &s
	}

	order := make([]string, len(r.order))
	copy(order, r.order)

	return &Registry{
		tools:   tools,
		order:   order,
		servers: servers,
		cfg:     r.cfg,
		filter:  filter,
		stats:   stats,
	}
}

func (r *Registry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		def := t.Definition()
		if r.filter != nil && !r.filter[def.Name] {
			continue
		}
		defs = append(defs, def)
	}
	return defs
}

func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	// Lazy init metrics on first execution
	r.mu.Lock()
	if r.toolCalls == nil {
		r.toolCalls = metrics.NewCounter("mcp_tool_calls_total", "Total MCP tool calls", map[string]string{})
		r.toolErrors = metrics.NewCounter("mcp_tool_errors_total", "Total MCP tool errors", map[string]string{})
		r.toolDuration = metrics.NewHistogram("mcp_tool_duration_seconds", "MCP tool execution duration", map[string]string{}, nil)
	}
	r.mu.Unlock()

	r.toolCalls.Inc()
	start := time.Now()
	result, err := tool.Execute(ctx, input)
	duration := time.Since(start)
	r.toolDuration.Observe(duration.Seconds())

	r.mu.Lock()
	s := r.stats[name]
	if s == nil {
		s = &ToolStats{}
		r.stats[name] = s
	}
	s.CallCount++
	s.LastCalledAt = time.Now()
	s.TotalDuration += duration
	if err != nil || (result != nil && result.IsError) {
		r.toolErrors.Inc()
		s.ErrorCount++
	}
	r.mu.Unlock()

	return result, err
}

// serverTool wraps an external MCP server tool for the Tool interface.
type serverTool struct {
	server *ServerClient
	def    ToolDefinition
}

func (st *serverTool) Definition() ToolDefinition {
	return st.def
}

func (st *serverTool) Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error) {
	return st.server.CallTool(ctx, st.def.Name, input)
}
