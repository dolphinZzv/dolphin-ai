// Package agentmesh implements Dolphin's multi-agent collaboration layer.
//
// It extends the A2A (Agent-to-Agent) JSON-RPC protocol so that one Dolphin
// process can delegate tasks to another. Phase 1 covers the message types,
// static registry, capability router, A2A client (with protocol negotiation),
// the sync Delegate path, sender-side rate limiting and the delegate_to_agent
// builtin tool.
package agentmesh

import (
	"encoding/json"
	"time"
)

// AgentStatus is the lifecycle state of an agent.
type AgentStatus string

const (
	AgentStarting AgentStatus = "starting"
	AgentRunning  AgentStatus = "running"
	AgentPaused   AgentStatus = "paused"
	AgentStopped  AgentStatus = "stopped"
	AgentError    AgentStatus = "error"
)

// AgentCard describes an agent's identity and capabilities.
type AgentCard struct {
	Name         string      `json:"name"`          // logical name
	Addr         string      `json:"addr"`          // host:port of the A2A server
	Capabilities []string    `json:"capabilities"`  // e.g. ["code-review", "golang"]
	Status       AgentStatus `json:"status"`        // starting | running | paused | stopped | error
	Load         int         `json:"load"`          // current concurrent tasks
	MaxLoad      int         `json:"max_load"`      // max concurrency
	Model        string      `json:"model"`         // LLM model in use
	Version      string      `json:"version"`       // dolphin version
	ProtoVersion int         `json:"proto_version"` // A2A protocol version (negotiation)
}

// MessageType is the kind of an AgentMessage envelope.
type MessageType string

const (
	MsgDelegate  MessageType = "delegate"  // delegate a task
	MsgQuery     MessageType = "query"     // query information
	MsgBroadcast MessageType = "broadcast" // broadcast notice
	MsgResult    MessageType = "result"    // return a result
	MsgHeartbeat MessageType = "heartbeat" // heartbeat
	MsgContext   MessageType = "context"   // shared context
	MsgCancel    MessageType = "cancel"    // cancel a running task
)

// AgentMessage is the wire envelope for inter-agent communication.
type AgentMessage struct {
	ID        string          `json:"id"`         // xid, unique message ID
	From      string          `json:"from"`       // sender agent URI
	To        string          `json:"to"`         // recipient agent URI (empty = broadcast)
	ReplyTo   string          `json:"reply_to"`   // correlation ID (request/reply pairing)
	Type      MessageType     `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp time.Time       `json:"timestamp"`
	TTL       int             `json:"ttl"` // gossip hop count, starts at 3
}

// ReplyMode controls how the delegator waits for the result.
type ReplyMode string

const (
	ReplySync   ReplyMode = "sync"   // block until the full result is ready
	ReplyAsync  ReplyMode = "async"  // return a task ID immediately, poll later
	ReplyStream ReplyMode = "stream" // SSE stream of intermediate events
)

// Priority is the urgency of a delegated task.
type Priority string

const (
	PriorityNormal     Priority = "normal"
	PriorityHigh       Priority = "high"
	PriorityBackground Priority = "background"
)

// FileMode selects how a shared file is transported.
type FileMode string

const (
	FileInline    FileMode = "inline"    // content embedded in JSON payload
	FileReference FileMode = "reference" // only a path is sent (shared fs / storage)
)

// SharedFile is a file attached to a delegation.
type SharedFile struct {
	Path    string   `json:"path"`
	Content string   `json:"content,omitempty"` // inline mode only
	Hash    string   `json:"hash,omitempty"`    // SHA256
	Mode    FileMode `json:"mode"`
}

// ContextMessage is a selected history message passed to a child agent.
type ContextMessage struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

// DelegateContext carries selectively-shared context (not the full history).
type DelegateContext struct {
	Messages []ContextMessage `json:"messages,omitempty"`
	Files    []SharedFile     `json:"files,omitempty"`
	State    map[string]any   `json:"state,omitempty"`
}

// DelegatePayload is the body of a delegate request.
type DelegatePayload struct {
	// ── task definition ──
	Task         string `json:"task"`                      // natural-language task (required)
	SystemPrompt string `json:"system_prompt,omitempty"`   // override receiver's system prompt
	MaxRounds    int    `json:"max_rounds"`                // max LLM rounds, default 50
	Timeout      string `json:"timeout"`                   // "30s" / "5m" / "1h", default "10m"

	// ── session linkage ──
	ParentSessionID string `json:"parent_session_id"` // required
	ChildSessionID  string `json:"child_session_id"`  // suggested prefix; receiver appends a suffix
	DelegationDepth int    `json:"delegation_depth"`  // +1 per hop

	// ── context ──
	Context DelegateContext `json:"context"`

	// ── tool control ──
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`

	// ── result shape ──
	ReplyMode    ReplyMode        `json:"reply_mode"`
	ResultSchema *json.RawMessage `json:"result_schema,omitempty"`

	// ── routing hints ──
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	PreferredAgent       string   `json:"preferred_agent,omitempty"`

	// ── priority ──
	Priority Priority `json:"priority"`
}

// DelegateStatus is the outcome state of a delegation.
type DelegateStatus string

const (
	DelegateCompleted DelegateStatus = "completed"
	DelegateFailed    DelegateStatus = "failed"
	DelegateTimeout   DelegateStatus = "timeout"
	DelegateCancelled DelegateStatus = "cancelled"
	DelegatePartial   DelegateStatus = "partial" // interrupted but produced a result
)

// ToolCallSummary is a one-line summary of a tool call made by the child agent.
type ToolCallSummary struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Summary string `json:"summary"`
}

// StreamEvent is one intermediate event in a streamed delegation.
type StreamEvent struct {
	Type    string    `json:"type"` // thinking | tool_call | text | progress
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

// DelegateResult is the body of a delegate response.
type DelegateResult struct {
	TaskID    string             `json:"task_id"`
	Status    DelegateStatus     `json:"status"`
	Content   string             `json:"content"`
	Rounds    int                `json:"rounds"`
	ToolCalls []ToolCallSummary  `json:"tool_calls,omitempty"`
	Error     *DelegateError     `json:"error,omitempty"`
	Events    []StreamEvent      `json:"events,omitempty"`
	Partial   *DelegateResult    `json:"partial,omitempty"` // partial result, if interrupted
}

// ErrorCode is the category of a delegation failure.
type ErrorCode string

const (
	ErrTimeout        ErrorCode = "timeout"          // timed out
	ErrAgentNotFound  ErrorCode = "agent_not_found"  // not in registry
	ErrAgentUnavail   ErrorCode = "agent_unavailable" // network unreachable / circuit open
	ErrAgentBusy      ErrorCode = "agent_busy"        // load >= max_load
	ErrCancelled      ErrorCode = "cancelled"         // cancelled by parent
	ErrDepthExceeded  ErrorCode = "depth_exceeded"    // over max_delegation_depth
	ErrPermission     ErrorCode = "permission"        // rejected by receiver (not in allowlist)
	ErrInternal       ErrorCode = "internal"          // receiver internal error (panic, OOM)
	ErrBadPayload     ErrorCode = "bad_payload"       // malformed request
	ErrRateLimited    ErrorCode = "rate_limited"      // throttled
)

// DelegateError describes a delegation failure.
type DelegateError struct {
	Code    ErrorCode        `json:"code"`
	Message string           `json:"message"`
	Agent   string           `json:"agent"`   // agent URI that failed
	Partial *DelegateResult  `json:"partial"` // partial result, if any
	Cause   string           `json:"cause"`   // underlying error description
}

// Error implements error.
func (e *DelegateError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != "" {
		return string(e.Code) + ": " + e.Message + " (" + e.Cause + ")"
	}
	return string(e.Code) + ": " + e.Message
}

// IsRetryable reports whether a failure of this code should be retried.
func (c ErrorCode) IsRetryable() bool {
	switch c {
	case ErrTimeout, ErrAgentUnavail, ErrAgentBusy:
		return true
	}
	return false
}

// SessionLink records a parent→child session relationship, stored on the
// parent agent side.
type SessionLink struct {
	ChildSessionID  string          `json:"child_session_id"`
	ParentSessionID string          `json:"parent_session_id"`
	ChildAgent      string          `json:"child_agent"`
	Status          DelegateStatus  `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
}
