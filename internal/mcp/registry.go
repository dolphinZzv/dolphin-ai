package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
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

// managedToolDef holds the factory and enabled check for a dynamically-managed built-in tool.
type managedToolDef struct {
	name      string
	factory   func(cfg *config.Config) Tool
	isEnabled func(cfg *config.Config) bool
}

// Registry manages all registered MCP tools, including external server tools.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	order   []string // registration order, used as tiebreaker in MostUsedTools
	servers []*ServerClient
	cfg     *config.MCPConfig
	fullCfg *config.Config  // latest full config for managed tool factories
	filter  map[string]bool // nil = no filter; non-nil = only allow listed tools
	stats   map[string]*ToolStats

	// managed tools that can be dynamically enabled/disabled
	managedTools map[string]*managedToolDef

	// lastConfigReload records when the config was last reloaded via OnConfigChange.
	lastConfigReload time.Time

	// metrics collectors (labeled by tool name)
	toolCalls    *metrics.LabeledCounter
	toolErrors   *metrics.LabeledCounter
	toolDuration *metrics.LabeledHistogram
	bus          *event.EventBus
}

func NewRegistry(cfg *config.Config) *Registry {
	return &Registry{
		tools:        make(map[string]Tool),
		servers:      make([]*ServerClient, 0),
		cfg:          &cfg.MCP,
		fullCfg:      cfg,
		stats:        make(map[string]*ToolStats),
		managedTools: make(map[string]*managedToolDef),
		toolCalls:    metrics.NewLabeledCounter("mcp_tool_calls_total", "Total MCP tool calls", "tool", nil),
		toolErrors:   metrics.NewLabeledCounter("mcp_tool_errors_total", "Total MCP tool errors", "tool", nil),
		toolDuration: metrics.NewLabeledHistogram("mcp_tool_duration_seconds", "MCP tool execution duration", "tool", nil, nil),
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

// RegisterManagedTool registers a built-in tool that can be dynamically enabled/disabled
// based on config hot-reload. The factory is called with the latest config when the tool
// is enabled, and the tool is removed from the registry when disabled.
func (r *Registry) RegisterManagedTool(name string, factory func(cfg *config.Config) Tool, isEnabled func(cfg *config.Config) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.managedTools[name] = &managedToolDef{
		name:      name,
		factory:   factory,
		isEnabled: isEnabled,
	}

	if isEnabled(r.fullCfg) {
		t := factory(r.fullCfg)
		r.tools[name] = t
		r.stats[name] = &ToolStats{}
		zap.S().Debugw("managed tool registered", "tool", name)
	}
}

// syncManagedTools enables or disables managed built-in tools based on the latest config.
// Must be called with r.mu held.
func (r *Registry) syncManagedTools(cfg *config.Config, mcpCfg *config.MCPConfig) {
	for name, def := range r.managedTools {
		if def.isEnabled(cfg) {
			if _, exists := r.tools[name]; !exists {
				t := def.factory(cfg)
				r.tools[name] = t
				r.stats[name] = &ToolStats{}
				zap.S().Infow("builtin tool enabled dynamically", "tool", name)
			}
		} else {
			if _, exists := r.tools[name]; exists {
				delete(r.tools, name)
				delete(r.stats, name)
				zap.S().Infow("builtin tool disabled dynamically", "tool", name)
			}
		}
	}
}

// SetEventBus sets the event bus for emitting MCP server notifications.
func (r *Registry) SetEventBus(bus *event.EventBus) {
	r.bus = bus
}

// configSubscriber is an optional interface for tools that need config change
// notifications to re-point stale config pointers or recreate resources.
type configSubscriber interface {
	OnConfigChange(oldCfg, newCfg *config.Config)
}

// OnConfigChange handles MCP config hot-reload. Re-points the config pointer,
// reloads external MCP servers if server definitions changed, and propagates
// the change to all registered tools that implement configSubscriber.
func (r *Registry) OnConfigChange(oldCfg, newCfg *config.Config) {
	serversChanged := !reflect.DeepEqual(oldCfg.MCP.Servers, newCfg.MCP.Servers)

	r.mu.Lock()
	r.cfg = &newCfg.MCP
	r.fullCfg = newCfg
	r.lastConfigReload = time.Now()
	r.syncManagedTools(newCfg, &newCfg.MCP)

	// Propagate to tools that implement configSubscriber.
	for _, tool := range r.tools {
		if cs, ok := tool.(configSubscriber); ok {
			cs.OnConfigChange(oldCfg, newCfg)
		}
	}
	r.mu.Unlock()

	if serversChanged {
		r.CloseServers()
		ctx := context.Background()
		if err := r.LoadServers(ctx); err != nil {
			zap.S().Warnw("mcp servers reload failed", "error", err)
		}
	}
}

// LoadServers starts external MCP servers defined in config and registers their tools.
// Failures for individual servers are logged as warnings and do not prevent other
// servers from loading. The caller should still defer CloseServers() to clean up
// any servers that did start successfully.
func (r *Registry) LoadServers(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var loaded, failed int
	for name, cfg := range r.cfg.Servers {
		if cfg.Enabled != nil && !*cfg.Enabled {
			zap.S().Debugw("mcp server skipped — disabled", "server", name)
			continue
		}
		client, err := NewServerClient(ctx, name, cfg, r.bus)
		if err != nil {
			failed++
			zap.S().Warnw("mcp server skipped — failed to create client", "server", name, "error", err)
			continue
		}

		listCtx, cancel := context.WithTimeout(ctx, config.TimeoutDuration(cfg.Timeout))
		defs, err := client.ListTools(listCtx)
		cancel()
		if err != nil {
			failed++
			client.Close()
			zap.S().Warnw("mcp server skipped — failed to list tools", "server", name, "error", err)
			continue
		}

		for _, def := range defs {
			def.Source = name
			wrapper := &serverTool{
				server: client,
				def:    def,
			}
			r.tools[name+":"+def.Name] = wrapper
			r.stats[name+":"+def.Name] = &ToolStats{}
			zap.S().Debugw("mcp tool registered", "tool", name+":"+def.Name, "server", name)
			if _, exists := r.tools[def.Name]; !exists {
				r.tools[def.Name] = wrapper
				r.stats[def.Name] = &ToolStats{}
				zap.S().Debugw("mcp tool registered (bare)", "tool", def.Name, "server", name)
			} else {
				zap.S().Warnw("mcp tool name collision — bare name skipped, use server:name prefix",
					"tool", def.Name, "server", name)
			}
		}

		r.servers = append(r.servers, client)
		loaded++
	}

	zap.S().Infow("mcp servers load complete", "loaded", loaded, "failed", failed)
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
		tools:        tools,
		order:        r.order,
		servers:      servers,
		cfg:          r.cfg,
		filter:       filter,
		stats:        stats,
		toolCalls:    r.toolCalls,
		toolErrors:   r.toolErrors,
		toolDuration: r.toolDuration,
		bus:          r.bus,
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

// LastConfigReload returns the time when the config was last reloaded.
func (r *Registry) LastConfigReload() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastConfigReload
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
		tools:        tools,
		order:        order,
		servers:      servers,
		cfg:          r.cfg,
		filter:       filter,
		stats:        stats,
		toolCalls:    r.toolCalls,
		toolErrors:   r.toolErrors,
		toolDuration: r.toolDuration,
		bus:          r.bus,
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

// MCPConfig returns a copy of the current MCP config. Safe to call concurrently.
// The returned value is a snapshot — call again to pick up hot-reload changes.
func (r *Registry) MCPConfig() config.MCPConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return *r.cfg
}

const tracerName = "dolphin/mcp"

func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	tr := otel.Tracer(tracerName)
	ctx, span := tr.Start(ctx, "mcp.tool.execute",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	span.SetAttributes(
		attribute.String("tool.name", name),
		attribute.String("input", truncateString(string(input), 1024)),
	)
	defer span.End()

	r.toolCalls.With(name).Inc()
	start := time.Now()
	result, err := tool.Execute(ctx, input)
	duration := time.Since(start)
	r.toolDuration.With(name).Observe(duration.Seconds())

	r.mu.Lock()
	s := r.stats[name]
	if s == nil {
		s = &ToolStats{}
		r.stats[name] = s
	}
	s.CallCount++
	s.LastCalledAt = time.Now()
	s.TotalDuration += duration
	hasError := err != nil || (result != nil && result.IsError)
	if hasError {
		r.toolErrors.With(name).Inc()
		s.ErrorCount++
	}
	r.mu.Unlock()

	var output string
	if result != nil {
		output = result.Content
	}
	if hasError {
		span.SetAttributes(
			attribute.Bool("tool.error", true),
			attribute.String("output", truncateString(output, 2048)),
		)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, truncateString(output, 256))
		}
	} else {
		span.SetAttributes(attribute.String("output", truncateString(output, 2048)))
		span.SetStatus(codes.Ok, "")
	}

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

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
