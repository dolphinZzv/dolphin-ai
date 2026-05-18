// Package buildin provides system built-in agents that are event-triggered
// and self-register via init() functions.
package buildin

import (
	"context"

	"dolphin/internal/event"
)

// BuildinAgent is the interface each built-in agent implements.
type BuildinAgent interface {
	Name() string
	Prompt() string
	// Init gives the agent a handle for dynamic event subscription
	// at runtime. Called once during coordinator startup.
	Init(ctx context.Context, handle *AgentHandle)
}

// AgentHandle gives buildin agents runtime access to the system.
// All actions are automatically recorded to session and OTel via the
// injected callbacks.
type AgentHandle struct {
	bus          *event.EventBus
	dispatchTask func(ctx context.Context, agentName, prompt string) (string, error)
	logEvent     func(ctx context.Context, evtType string, data map[string]any)
	startSpan    func(ctx context.Context, agentName, triggerEvent string) func()
}

// NewAgentHandle creates a new AgentHandle.
func NewAgentHandle(
	bus *event.EventBus,
	dispatchTask func(ctx context.Context, agentName, prompt string) (string, error),
	logEvent func(ctx context.Context, evtType string, data map[string]any),
	startSpan func(ctx context.Context, agentName, triggerEvent string) func(),
) *AgentHandle {
	return &AgentHandle{
		bus:          bus,
		dispatchTask: dispatchTask,
		logEvent:     logEvent,
		startSpan:    startSpan,
	}
}

// Subscribe subscribes to an event. Returns an unsubscribe function.
func (h *AgentHandle) Subscribe(t event.Type, handler func(ctx context.Context, evt event.Event)) (unsubscribe func()) {
	return h.bus.On(t, handler)
}

// RegisterEventType documents a new event type. Since event.Type is string,
// any string can be emitted as an event — this provides documentation.
func (h *AgentHandle) RegisterEventType(t event.Type, description string) {}

// Emit emits a custom event.
func (h *AgentHandle) Emit(ctx context.Context, evt event.Event) {
	h.bus.Emit(ctx, evt)
}

// DispatchTask dispatches a task and auto-records to session + OTel.
// Returns the task ID.
func (h *AgentHandle) DispatchTask(ctx context.Context, agentName, triggerEvent, prompt string) (string, error) {
	h.logEvent(ctx, "agent_action", map[string]any{
		"agent":  agentName,
		"event":  triggerEvent,
		"status": "dispatched",
	})

	endSpan := h.startSpan(ctx, agentName, triggerEvent)
	defer endSpan()

	taskID, err := h.dispatchTask(ctx, agentName, prompt)
	if err != nil {
		h.logEvent(ctx, "agent_action", map[string]any{
			"agent":  agentName,
			"event":  triggerEvent,
			"status": "failed",
			"error":  err.Error(),
		})
		return "", err
	}

	h.logEvent(ctx, "agent_action", map[string]any{
		"agent":   agentName,
		"event":   triggerEvent,
		"status":  "completed",
		"task_id": taskID,
	})
	return taskID, nil
}

// BuildinRegistry holds all registered built-in agents.
type BuildinRegistry struct {
	agents map[string]BuildinAgent
}

var global = NewBuildinRegistry()

// NewBuildinRegistry creates a new empty registry.
func NewBuildinRegistry() *BuildinRegistry {
	return &BuildinRegistry{agents: make(map[string]BuildinAgent)}
}

// Register adds a buildin agent to the global registry. Called from init().
func Register(a BuildinAgent) {
	global.agents[a.Name()] = a
}

// GetRegistry returns the global registry.
func GetRegistry() *BuildinRegistry {
	return global
}

// List returns all registered buildin agents.
func (r *BuildinRegistry) List() []BuildinAgent {
	list := make([]BuildinAgent, 0, len(r.agents))
	for _, a := range r.agents {
		list = append(list, a)
	}
	return list
}

