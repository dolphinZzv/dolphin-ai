package workflow

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// loadResult reads a .result.yaml file if it exists.
func loadResult(path string) (*WorkflowResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wr WorkflowResult
	if err := yaml.Unmarshal(data, &wr); err != nil {
		return nil, fmt.Errorf("workflow: invalid result file %s: %w", path, err)
	}
	wr.FilePath = path
	return &wr, nil
}

// writeResult writes the workflow result to the .result.yaml file.
func writeResult(spec *WorkflowSpec, rs *runState, status string, startedAt time.Time) error {
	wr := WorkflowResult{
		Workflow: spec.Name,
		Status:   status,
		Duration: time.Since(startedAt).Round(time.Millisecond).String(),
		Steps:    buildStepResults(rs),
	}

	data, err := yaml.Marshal(wr)
	if err != nil {
		return fmt.Errorf("workflow: marshal result: %w", err)
	}

	// Result file writes alongside the workflow file.
	// For brain-based workflows, the path is derived from the brain directory.
	path := spec.Name + ".result.yaml"
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("workflow: write result: %w", err)
	}
	return nil
}

func buildStepResults(rs *runState) []StepResult {
	var steps []StepResult
	for _, id := range rs.order {
		if sr, ok := rs.results[id]; ok {
			steps = append(steps, *sr)
		}
	}
	// Include steps not yet in order (e.g. just started).
	for id, sr := range rs.results {
		found := false
		for _, oid := range rs.order {
			if oid == id {
				found = true
				break
			}
		}
		if !found {
			steps = append(steps, *sr)
		}
	}
	return steps
}

// restoreState populates the run state from a previous partial result.
func restoreState(spec *WorkflowSpec, prev *WorkflowResult, rs *runState) {
	for _, sr := range prev.Steps {
		status := sr.Status
		if status == StatusRunning {
			// Steps that were mid-execution during a crash must be re-run.
			status = StatusPending
		}
		rs.statuses[sr.ID] = status
		cp := sr
		cp.Status = status
		rs.results[sr.ID] = &cp

		if len(sr.Instances) > 0 {
			rs.instance[sr.ID] = make(map[string]*InstanceResult)
			for i := range sr.Instances {
				inst := sr.Instances[i]
				cp := inst
				rs.instance[sr.ID][inst.Key] = &cp
			}
		}

		// Mark step as ordered.
		found := false
		for _, id := range rs.order {
			if id == sr.ID {
				found = true
				break
			}
		}
		if !found {
			rs.order = append(rs.order, sr.ID)
		}
	}
}
