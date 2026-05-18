package agent

import "time"

// AgentKind distinguishes user-created agents from coordinator-created ones.
type AgentKind int

const (
	AgentUser    AgentKind = iota // from .dolphin/agents/<name>/
	AgentCoord                    // created by coordinator at runtime
	AgentBuildin                  // system built-in, event-triggered
)

func (k AgentKind) String() string {
	switch k {
	case AgentUser:
		return "user"
	case AgentCoord:
		return "temp"
	case AgentBuildin:
		return "buildin"
	default:
		return "unknown"
	}
}

// AgentDef defines an agent that can be spawned by the pool.
type AgentDef struct {
	Name      string   `yaml:"name" json:"name"`
	Role      string   `yaml:"role" json:"role"`
	Tools     []string `yaml:"tools" json:"tools"`
	Model     string   `yaml:"model,omitempty" json:"model,omitempty"`
	Workspace string   `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Timeout   int      `yaml:"timeout,omitempty" json:"timeout,omitempty"` // per-task timeout (seconds)
}

// Task is a unit of work sent to an agent.
type Task struct {
	ID      string `json:"id"`
	Input   string `json:"input"`
	Timeout int    `json:"timeout,omitempty"` // seconds, 0 = use agent default
}

// TaskResult is the structured result from a completed agent task.
type TaskResult struct {
	TaskID     string `json:"task_id"`
	AgentName  string `json:"agent_name"`
	Success    bool   `json:"success"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"` // completed / cancelled / timeout / error
}

// AgentStatus tracks runtime state of an agent in the pool.
type AgentStatus struct {
	Name       string    `json:"name"`
	Kind       string    `json:"kind"` // "user" or "temp"
	Role       string    `json:"role"`
	Status     string    `json:"status"` // idle / busy / error
	TasksDone  int       `json:"tasks_done"`
	Workspace  string    `json:"workspace"`
	CreatedAt  time.Time `json:"created_at"`
	LastTaskAt time.Time `json:"last_task_at,omitempty"`
}
