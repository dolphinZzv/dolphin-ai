package agent

import "time"

// Priority controls task scheduling order within an agent's queue.
type Priority int

const (
	PriLow      Priority = 0
	PriNormal   Priority = 1
	PriHigh     Priority = 2
	PriCritical Priority = 3
)

// TaskEventType labels events during a task lifecycle.
type TaskEventType string

const (
	TaskDispatched TaskEventType = "dispatched"
	TaskProcessing TaskEventType = "processing"
	TaskToolCall   TaskEventType = "tool_call"
	TaskToolResult TaskEventType = "tool_result"
	TaskCompleted  TaskEventType = "completed"
	TaskFailed     TaskEventType = "failed"
	TaskCancelled  TaskEventType = "cancelled"
)

// TaskEvent records a single event in a task lifecycle.
type TaskEvent struct {
	Type      TaskEventType `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Detail    string        `json:"detail,omitempty"`
}

// AgentKind distinguishes user-created agents from coordinator-created ones.
type AgentKind int

const (
	AgentUser    AgentKind = iota // from .dolphin/agents/<name>/
	AgentCoord                    // created by coordinator at runtime
	AgentBuildin                  // system built-in, event-triggered
	AgentScope                    // registered from scopes.yaml
)

func (k AgentKind) String() string {
	switch k {
	case AgentUser:
		return "user"
	case AgentCoord:
		return "temp"
	case AgentBuildin:
		return "buildin"
	case AgentScope:
		return "scope"
	default:
		return "unknown"
	}
}

// AgentDef defines an agent that can be spawned by the pool.
type AgentDef struct {
	Name      string   `yaml:"name" json:"name"`
	Role      string   `yaml:"role" json:"role"`
	Tools     []string `yaml:"tools" json:"tools"`
	Skills    []string `yaml:"skills,omitempty" json:"skills,omitempty"`       // visible skills (empty = all)
	Workflows []string `yaml:"workflows,omitempty" json:"workflows,omitempty"` // visible workflows (empty = all)
	Model     string   `yaml:"model,omitempty" json:"model,omitempty"`
	Workspace string   `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Timeout   int      `yaml:"timeout,omitempty" json:"timeout,omitempty"` // per-task timeout (seconds)
	Group     string   `yaml:"group,omitempty" json:"group,omitempty"`     // concurrency group, e.g. "research", "coding"
}

// Task is a unit of work sent to an agent.
type Task struct {
	ID       string   `json:"id"`
	Input    string   `json:"input"`
	Timeout  int      `json:"timeout,omitempty"`  // seconds, 0 = use agent default
	Priority Priority `json:"priority,omitempty"` // scheduling priority, 0 = normal
}

// TaskResult is the structured result from a completed agent task.
type TaskResult struct {
	TaskID     string      `json:"task_id"`
	AgentName  string      `json:"agent_name"`
	Success    bool        `json:"success"`
	Output     string      `json:"output"`
	Error      string      `json:"error,omitempty"`
	DurationMs int64       `json:"duration_ms"`
	Status     string      `json:"status"` // completed / cancelled / timeout / error
	Events     []TaskEvent `json:"events,omitempty"`
}

// AgentMessage is a message sent between agents in the pool.
type AgentMessage struct {
	From    string    `json:"from"`
	To      string    `json:"to"`      // "" = broadcast
	Subject string    `json:"subject"` // message type label
	Body    string    `json:"body"`
	SentAt  time.Time `json:"sent_at"`
}

// AgentStatus tracks runtime state of an agent in the pool.
type AgentStatus struct {
	Name          string    `json:"name"`
	Kind          string    `json:"kind"` // "user" or "temp"
	Role          string    `json:"role"`
	Status        string    `json:"status"` // idle / busy / error
	TasksDone     int       `json:"tasks_done"`
	Workspace     string    `json:"workspace"`
	CreatedAt     time.Time `json:"created_at"`
	LastTaskAt    time.Time `json:"last_task_at,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	CurrentTaskID string    `json:"current_task_id,omitempty"`
	Tools         []string  `json:"tools,omitempty"`
}
