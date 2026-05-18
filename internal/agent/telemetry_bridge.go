package agent

import "context"

// TelemetryCallbacks are set by the telemetry package during init to avoid
// circular imports. All callbacks are nil-safe — callers should guard with != nil.
var TelemetryCallbacks = struct {
	OnCompression    func()
	OnFailover       func(from, to string)
	OnPoolSize       func(n int64)
	OnActiveAgents   func(n int64)
	OnTaskDispatched func(agentName string)
	OnTaskCompleted  func(agentName string, success bool)

	// Span callbacks return an end func that must be called to close the span.
	OnCompressionSpan func(ctx context.Context, sessionID string, turn int) (end func())
	OnDispatchSpan    func(ctx context.Context, agentName string) (end func())
	OnTaskSpan        func(ctx context.Context, agentName, taskID string) (end func())
	OnBuildinSpan     func(ctx context.Context, agentName, triggerEvent string) (end func())
}{}
