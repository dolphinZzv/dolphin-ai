package context

import stdctx "context"

// Workflow injects guidance about when and how to use .workflow.yaml.
type Workflow struct{}

func (s *Workflow) Name() string { return "workflow" }
func (s *Workflow) Index() int   { return 8 }

func (s *Workflow) BuildContent(_ stdctx.Context) (string, error) {
	return `## Workflow

When a task involves multiple sub-steps that can benefit from parallelism or structured DAG execution, you MUST create a .workflow.yaml file via brain_write and then execute it via run_workflow. Do NOT call tools one by one serially when the task is decomposable.

### Creating the workflow file

1. Call brain_write to create a file named <name>.workflow.yaml (e.g. audit.workflow.yaml) under the workflow/ directory.
2. Call run_workflow with the path: workflow/<name>.workflow.yaml

### Required format

` + "```yaml\n" + `version: "1"
name: task-name        # unique name, used for .result.yaml
description: "..."     # optional, shown in progress
steps:
  - id: step_id        # unique within this workflow
    prompt: "..."       # what this step should do (required)
    output_schema:     # optional JSON schema for structured output
      field: type      #   e.g. {url: string, time_seconds: number}
    depends_on: []     # optional, step IDs this step waits for
    foreach: "$step.field"  # optional, see foreach section below
    checkpoint: false  # optional, pause after this step for review
    timeout: "60s"     # optional, per-step timeout (default 300s, 0 = no timeout)
` + "```\n" + `

### Template variables

Use the shortcut syntax to reference upstream step results in a step's prompt:

| Shortcut       | Meaning                             |
|----------------|-------------------------------------|
| $step.field    | Value of "field" from step "step"   |
| $each          | Current element value (inside foreach) |
| $each.key      | Current element's key (inside foreach) |
| $step[*].field | All instances' "field" values, joined |

Example:
` + "```yaml\n" + `- id: summarize
  prompt: "Compare $step_a.time vs $step_b.time, which is fastest?"
  depends_on: [step_a, step_b]
` + "```\n" + `
Note: The $step.field shortcuts are converted to Go template syntax internally. You can also use {{.step.field}} directly.

### Parallelism

Steps without depends_on run concurrently. Steps with depends_on wait for those dependencies to finish before starting.

### Foreach (dynamic fan-out)

When a step should be repeated for each element of a list produced by an upstream step, use foreach:

` + "```yaml\n" + `- id: list_files
  prompt: "list all .go files, output {files: [string, ...]}"
  output_schema: {files: [string]}

- id: audit_file
  prompt: "audit $each for security issues"
  foreach: "$list_files.files"
  depends_on: [list_files]
` + "```\n" + `

The foreach step is expanded into N parallel instances, one per element. The current element is available as the template variable $each.

### Checkpoint (pause for review)

A step with checkpoint: true causes the engine to pause after that step completes. The .result.yaml is written to disk and run_workflow returns a checkpoint-reached message. You can then review the partial results, optionally edit the .workflow.yaml (add/remove/modify steps that haven't run yet), and call continue_workflow to resume.

` + "```yaml\n" + `- id: dangerous_action
  prompt: "..."
  checkpoint: true
` + "```\n" + `

### File lifecycle

- .workflow.yaml — written by you via brain_write, read by run_workflow/continue_workflow
- .result.yaml — written by the engine, inspect it to see progress and step outputs
- After completion, .result.yaml contains the final status: "completed"`, nil
}
