package workflow

import (
	"fmt"
	"time"
)

// validateContinue checks that completed steps in the previous result have not been removed.
func validateContinue(prev *WorkflowResult, spec *WorkflowSpec) error {
	specSteps := make(map[string]bool, len(spec.Steps))
	for _, s := range spec.Steps {
		specSteps[s.ID] = true
	}

	for _, ps := range prev.Steps {
		if ps.Status != StatusDone && ps.Status != StatusFailed && ps.Status != StatusSkipped {
			continue // was still pending, can be removed or modified
		}
		if !specSteps[ps.ID] {
			return fmt.Errorf("workflow: cannot continue: completed step %q has been removed from the workflow", ps.ID)
		}
	}
	return nil
}

// buildRunState creates a fresh runState from a WorkflowSpec.
func newRunState(spec *WorkflowSpec) *runState {
	rs := &runState{
		spec:     spec,
		statuses: make(map[string]StepStatus),
		results:  make(map[string]*StepResult),
		instance: make(map[string]map[string]*InstanceResult),
	}

	for _, s := range spec.Steps {
		rs.statuses[s.ID] = StatusPending
		rs.results[s.ID] = &StepResult{ID: s.ID, Status: StatusPending}
		rs.order = append(rs.order, s.ID)
	}
	return rs
}

// markRunning sets a step status to running.
func (rs *runState) markRunning(stepID string) {
	rs.statuses[stepID] = StatusRunning
	rs.results[stepID].Status = StatusRunning
}

// markDone records a successful step completion.
func (rs *runState) markDone(stepID string, instances []InstanceResult, duration time.Duration, result any) {
	rs.statuses[stepID] = StatusDone
	sr := rs.results[stepID]
	sr.Status = StatusDone
	sr.Duration = duration.Round(time.Millisecond).String()
	sr.Instances = instances
	sr.Result = result
}

// markFailed records a step failure.
func (rs *runState) markFailed(stepID string, errMsg string) {
	rs.statuses[stepID] = StatusFailed
	rs.results[stepID].Status = StatusFailed
	rs.results[stepID].Error = errMsg
}

// markSkipped marks a step as skipped (upstream failed).
func (rs *runState) markSkipped(stepID string) {
	rs.statuses[stepID] = StatusSkipped
	rs.results[stepID].Status = StatusSkipped
}

// findReady returns step IDs whose dependencies are all done.
func (rs *runState) findReady() []string {
	spec := rs.spec
	var ready []string
	for _, s := range spec.Steps {
		if rs.statuses[s.ID] != StatusPending {
			continue
		}
		allDepsDone := true
		for _, dep := range s.DependsOn {
			st := rs.statuses[dep]
			if st != StatusDone {
				allDepsDone = false
				break
			}
		}
		if allDepsDone {
			ready = append(ready, s.ID)
		}
	}
	return ready
}

// allDone returns true when every step is in a terminal state.
func (rs *runState) allDone() bool {
	for _, s := range rs.spec.Steps {
		switch rs.statuses[s.ID] { //nolint:exhaustive // non-terminal states fall through to default (not done)
		case StatusDone, StatusFailed, StatusSkipped:
			continue
		default:
			return false
		}
	}
	return true
}

// skipDependents marks all downstream steps of a failed step as skipped.
func (rs *runState) skipDependents(failedID string) {
	for _, s := range rs.spec.Steps {
		if rs.statuses[s.ID] != StatusPending {
			continue
		}
		for _, dep := range s.DependsOn {
			if dep == failedID {
				rs.markSkipped(s.ID)
				rs.skipDependents(s.ID) // recursive
				break
			}
			// Also skip if any dependency was skipped.
			if rs.statuses[dep] == StatusSkipped {
				rs.markSkipped(s.ID)
				rs.skipDependents(s.ID)
				break
			}
		}
	}
}

func (rs *runState) hasFailures() bool {
	for _, s := range rs.spec.Steps {
		if rs.statuses[s.ID] == StatusFailed {
			return true
		}
	}
	return false
}

func (rs *runState) failReason() string {
	for _, s := range rs.spec.Steps {
		if rs.statuses[s.ID] == StatusFailed {
			return fmt.Sprintf("step %q failed: %s", s.ID, rs.results[s.ID].Error)
		}
	}
	return "unknown failure"
}
