package workflow

import "fmt"

// StepStatus enumerates the lifecycle states of a workflow step.
type StepStatus string

const (
	StatusPending StepStatus = "pending"
	StatusReady   StepStatus = "ready"
	StatusRunning StepStatus = "running"
	StatusDone    StepStatus = "done"
	StatusFailed  StepStatus = "failed"
	StatusSkipped StepStatus = "skipped"
)

// WorkflowSpec is the YAML schema for a .workflow.yaml file.
type WorkflowSpec struct {
	Version     string     `yaml:"version"`
	Name        string     `yaml:"name"`
	Description string     `yaml:"description,omitempty"`
	Steps       []StepSpec `yaml:"steps"`
}

// StepSpec describes a single step as parsed from YAML.
type StepSpec struct {
	ID           string         `yaml:"id"`
	Prompt       string         `yaml:"prompt"`
	DependsOn    []string       `yaml:"depends_on,omitempty"`
	ForEach      string         `yaml:"foreach,omitempty"`
	OutputSchema map[string]any `yaml:"output_schema,omitempty"`
	Timeout      string         `yaml:"timeout,omitempty"`
	MaxTokens    int            `yaml:"max_tokens,omitempty"`
	Checkpoint   bool           `yaml:"checkpoint,omitempty"`
}

// StepResult holds the outcome of one step (or foreach step with instances).
type StepResult struct {
	ID        string           `yaml:"id"`
	Status    StepStatus       `yaml:"status"`
	Duration  string           `yaml:"duration,omitempty"`
	Result    any              `yaml:"result,omitempty"`
	Error     string           `yaml:"error,omitempty"`
	Instances []InstanceResult `yaml:"instances,omitempty"`
}

// InstanceResult holds the outcome of one foreach instance.
type InstanceResult struct {
	Key      string     `yaml:"key"`
	Status   StepStatus `yaml:"status"`
	Duration string     `yaml:"duration,omitempty"`
	Result   any        `yaml:"result,omitempty"`
	Error    string     `yaml:"error,omitempty"`
}

// WorkflowResult is the full result persisted to {name}.result.yaml.
type WorkflowResult struct {
	Workflow string       `yaml:"workflow"`
	Status   string       `yaml:"status"` // running, completed, paused, failed
	Duration string       `yaml:"duration,omitempty"`
	Steps    []StepResult `yaml:"steps"`
	FilePath string       `yaml:"-"` // internal: path to the .workflow.yaml
}

// stepInstance is an internal type representing one execution unit.
type stepInstance struct {
	StepID   string
	Key      string // foreach key, or stepID for non-foreach
	Prompt   string
	Timeout  string
	MaxTokens int
	Spec     StepSpec
	Each     any // the current foreach element value
}

// ErrCheckpointReached is returned by Engine.Run when a checkpoint step completes.
var ErrCheckpointReached = fmt.Errorf("checkpoint reached")

// runState holds the mutable execution state for a single workflow run.
type runState struct {
	spec     *WorkflowSpec
	statuses map[string]StepStatus           // stepID → status
	results  map[string]*StepResult           // stepID → result (accumulated)
	instance map[string]map[string]*InstanceResult // stepID → key → result
	order    []string                         // step execution order (for foreach expansion)
}
