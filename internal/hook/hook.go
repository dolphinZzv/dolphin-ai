// Package hook provides synchronous interception points for the agent loop.
// Plugins register handlers that can modify context or abort the flow.
package hook

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Point identifies a named interception point in the agent loop.
type Point string

const (
	PointSessionStart   Point = "session:start"
	PointSessionEnd     Point = "session:end"
	PointUserInput      Point = "user:input"
	PointBeforeLLM      Point = "llm:before"
	PointAfterLLM       Point = "llm:after"
	PointBeforeTool     Point = "tool:before"
	PointAfterTool      Point = "tool:after"
	PointBeforeResponse Point = "response:before"
	PointOnError        Point = "error"

	// Scheduler hook points
	PointSchedulerTaskBefore Point = "scheduler:task:before"
	PointSchedulerTaskAfter  Point = "scheduler:task:after"

	// Transport hook points
	PointTransportConnect    Point = "transport:connect"
	PointTransportDisconnect Point = "transport:disconnect"
	PointTransportReceive    Point = "transport:receive"
	PointTransportSend       Point = "transport:send"
)

// Abortable returns true if returning an error from this hook point aborts the flow.
func (p Point) Abortable() bool {
	switch p {
	case PointUserInput, PointBeforeLLM, PointBeforeTool:
		return true
	default:
		return false
	}
}

// Handler is a hook callback. Return error to abort flow at abortable points.
type Handler func(ctx context.Context, hc *Context) error

// Context is passed to hook handlers. Fields are only populated at their
// relevant hook point (see hc.Point).
type Context struct {
	Point     Point
	SessionID string
	Turn      int

	// Mutable at specific points:
	UserInput string          // user:input — may be rewritten
	Request   any             // llm:before — *ProviderRequest, may modify fields
	ToolName  string          // tool:* — name of the tool being called
	ToolArgs  json.RawMessage // tool:before — may be rewritten

	// Read-only:
	Response   any   // llm:after — *ProviderResponse
	ToolResult any   // tool:after — *ToolResult
	Error      error // error

	// Scheduler fields (scheduler:task:before / scheduler:task:after)
	TaskName  string // cron task name
	TaskInput string // cron task prompt/input

	// Transport fields (transport:*)
	TransportName string // transport name (e.g. "stdio", "ssh", "dingtalk")
	UserOutput    string // transport:send — response content sent to transport

	// Values persists across hook points within a single turn. Plugins use it
	// to pass data from one point to another (e.g. timing from tool:before → tool:after).
	Values map[string]any
}

// Registration pairs a handler with its priority.
type Registration struct {
	Priority int
	Handler  Handler
}

// Registry holds ordered hook registrations per point.
type Registry struct {
	mu    sync.RWMutex
	hooks map[Point][]Registration
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{hooks: make(map[Point][]Registration)}
}

// Register adds a hook handler for the given point. Handlers fire in priority
// order (lower values first). Concurrent registration is safe.
func (r *Registry) Register(point Point, priority int, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[point] = append(r.hooks[point], Registration{Priority: priority, Handler: h})
	sort.SliceStable(r.hooks[point], func(i, j int) bool {
		return r.hooks[point][i].Priority < r.hooks[point][j].Priority
	})
}

// Fire runs all handlers registered for point, in priority order.
// Returns the first error. If the point is not abortable, errors are logged
// but not returned.
func (r *Registry) Fire(ctx context.Context, point Point, hc *Context) error {
	hc.Point = point
	r.mu.RLock()
	regs := make([]Registration, len(r.hooks[point]))
	copy(regs, r.hooks[point])
	r.mu.RUnlock()

	for _, reg := range regs {
		if err := reg.Handler(ctx, hc); err != nil {
			if point.Abortable() {
				return err
			}
			zap.S().Debugw("hook error (non-aborting)", "point", string(point), "error", err)
		}
	}
	return nil
}

// HasAny returns true if at least one hook is registered for any of the given points.
func (r *Registry) HasAny(points ...Point) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range points {
		if len(r.hooks[p]) > 0 {
			return true
		}
	}
	return false
}
