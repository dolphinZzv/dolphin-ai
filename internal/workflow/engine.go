package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dolphin/internal/agentio"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/i18n"
	"dolphin/internal/llm"
	"dolphin/internal/tool"

	"go.uber.org/zap"
)

// Engine executes workflow YAML files, scheduling steps according to their DAG dependencies.
type Engine struct {
	toolReg     *tool.Registry
	llmProvider llm.Provider
	eventBus    *event.Bus
	logger      *zap.Logger

	agentIO     *agentio.AgentIO
	config      *config.Config
	onProgress  func(agentio.TurnResult)
}

// NewEngine creates a new workflow Engine.
func NewEngine(
	toolReg *tool.Registry,
	llmProvider llm.Provider,
	eventBus *event.Bus,
	logger *zap.Logger,

	agentIO *agentio.AgentIO,
	cfg *config.Config,
) *Engine {
	return &Engine{
		toolReg:     toolReg,
		llmProvider: llmProvider,
		eventBus:    eventBus,
		logger:      logger,

		agentIO:     agentIO,
		config:      cfg,
	}
}

// SetOnProgress sets a callback for streaming workflow progress messages.
func (e *Engine) SetOnProgress(fn func(agentio.TurnResult)) {
	e.onProgress = fn
}

func (e *Engine) progress(transportID, text string) {
	if e.onProgress != nil {
		e.onProgress(agentio.TurnResult{
			TransportID: transportID,
			Text:        text,
		})
	}
}

// Run executes a workflow from parsed spec, returning results on completion.
// Returns ErrCheckpointReached when a checkpoint step completes.
// transportID is used for progress streaming to the correct transport.
func (e *Engine) Run(ctx context.Context, spec *WorkflowSpec, transportID string) (*WorkflowResult, error) {
	startedAt := time.Now()

	rs := newRunState(spec)

	// Check for existing result file (crash recovery or checkpoint resume).
	if prev, err := loadResult(spec.Name + ".result.yaml"); err == nil && (prev.Status == "running" || prev.Status == "paused") {
		restoreState(spec, prev, rs)
		e.logger.Info("workflow resumed from partial result",
			zap.String("name", spec.Name),
			zap.Int("steps_done", countDone(rs)),
		)
		e.progress(transportID, i18n.T("workflow.resume", spec.Name, countDone(rs), len(spec.Steps)))
	}

	e.progress(transportID, i18n.T("workflow.started", spec.Name, len(spec.Steps)))
	e.publishEvent(ctx, event.EventWorkflowStart, spec.Name, nil)

	poolSize := e.config.GetInt("agent.pool_size")
	if poolSize <= 0 {
		poolSize = 4
	}
	sem := make(chan struct{}, poolSize)

	for !rs.allDone() {
		ready := rs.findReady()
		if len(ready) == 0 {
			if rs.allDone() {
				break
			}
			// Deadlock: no ready steps but work remains.
			reason := rs.failReason()
			e.progress(transportID, i18n.T("workflow.failed", spec.Name, reason))
			return e.finish(spec, rs, "failed", startedAt), fmt.Errorf("workflow: %s", reason)
		}

		var checkpointReached bool
		var wg sync.WaitGroup
		var mu sync.Mutex

		// Pre-compute instances for all ready steps before dispatching goroutines.
		type readyStep struct {
			spec      StepSpec
			instances []stepInstance
		}
		readySteps := make([]readyStep, 0, len(ready))
		for _, stepID := range ready {
			step := rs.specLookup(stepID)
			if step == nil {
				continue
			}
			instances, err := e.expandForeach(*step, rs)
			if err != nil {
				rs.markFailed(stepID, err.Error())
				rs.skipDependents(stepID)
				e.progress(transportID, i18n.T("workflow.step_failed", spec.Name, stepID, err.Error()))
				continue
			}
			readySteps = append(readySteps, readyStep{spec: *step, instances: instances})
		}

		// Mark all ready steps as running before dispatching.
		for _, rstep := range readySteps {
			rs.markRunning(rstep.spec.ID)
			e.progress(transportID, i18n.T("workflow.step_running", spec.Name, rstep.spec.ID))
			e.publishStepEvent(ctx, spec.Name, rstep.spec.ID, StatusRunning, nil)
			if len(rstep.instances) > 1 {
				e.progress(transportID, i18n.T("workflow.foreach_expand", spec.Name, rstep.spec.ID, len(rstep.instances)))
			}
		}

		for _, rstep := range readySteps {
			wg.Add(1)
			go func(s StepSpec, insts []stepInstance) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				var instResults []InstanceResult
				for _, inst := range insts {
					if ctx.Err() != nil {
						instResults = append(instResults, InstanceResult{
							Key: inst.Key, Status: StatusSkipped,
							Error: ctx.Err().Error(),
						})
						continue
					}
					res := e.executeStep(ctx, inst)
					instResults = append(instResults, *res)
					if res.Status == StatusDone {
						e.progress(transportID, i18n.T("workflow.instance_done", spec.Name, inst.Key))
					}
				}

				mu.Lock()
				if allInstDone(instResults) {
					// For non-foreach steps, use the raw result.
					var result any
					if len(instResults) == 1 && instResults[0].Key == s.ID {
						result = instResults[0].Result
					}
					rs.markDone(s.ID, instResults, time.Since(startedAt), result)

					// Store foreach instance results.
					for _, ir := range instResults {
						if rs.instance[s.ID] == nil {
							rs.instance[s.ID] = make(map[string]*InstanceResult)
						}
						cp := ir
						rs.instance[s.ID][ir.Key] = &cp
					}

					e.progress(transportID, i18n.T("workflow.step_done", spec.Name, s.ID, rs.results[s.ID].Duration))
					e.publishStepEvent(ctx, spec.Name, s.ID, StatusDone, result)
				} else if allInstFailed(instResults) {
					errMsg := firstError(instResults)
					rs.markFailed(s.ID, errMsg)
					rs.skipDependents(s.ID)
					e.progress(transportID, i18n.T("workflow.step_failed", spec.Name, s.ID, errMsg))
				} else {
					// Partial success — mark failed.
					errMsg := "partial failure"
					rs.markFailed(s.ID, errMsg)
					e.progress(transportID, i18n.T("workflow.step_failed", spec.Name, s.ID, errMsg))
				}

				// Write incremental result.
				if err := writeResult(spec, rs, "running", startedAt); err != nil {
					e.logger.Warn("workflow: write incremental result", zap.Error(err))
				}

				if s.Checkpoint && rs.statuses[s.ID] == StatusDone {
					checkpointReached = true
				}
				mu.Unlock()
			}(rstep.spec, rstep.instances)
		}

		wg.Wait()

		if checkpointReached {
			e.progress(transportID, i18n.T("workflow.checkpoint_pause", spec.Name))
			e.publishEvent(ctx, event.EventWorkflowPaused, spec.Name, nil)
			writeResult(spec, rs, "paused", startedAt)
			return nil, ErrCheckpointReached
		}
	}

	// All done.
	status := "completed"
	var runErr error
	if rs.hasFailures() {
		status = "failed"
		runErr = fmt.Errorf("workflow: %s", rs.failReason())
	}
	duration := time.Since(startedAt)
	if runErr != nil {
		e.progress(transportID, i18n.T("workflow.failed", spec.Name, rs.failReason()))
		e.publishEvent(ctx, event.EventWorkflowComplete, spec.Name, nil)
	} else {
		e.progress(transportID, i18n.T("workflow.completed", spec.Name, duration.Round(time.Millisecond).String()))
		e.publishEvent(ctx, event.EventWorkflowComplete, spec.Name, nil)
	}
	return e.finish(spec, rs, status, startedAt), runErr
}

// Continue resumes a workflow from its last checkpoint.
func (e *Engine) Continue(ctx context.Context, spec *WorkflowSpec, transportID string) (*WorkflowResult, error) {
	// Load previous result.
	prev, err := loadResult(spec.Name + ".result.yaml")
	if err != nil {
		return nil, fmt.Errorf("workflow: cannot continue: %w", err)
	}
	if prev.Status != "paused" {
		return nil, fmt.Errorf("workflow: cannot continue workflow with status %q", prev.Status)
	}
	// Validate that completed steps haven't been modified.
	if err := validateContinue(prev, spec); err != nil {
		return nil, err
	}
	return e.Run(ctx, spec, transportID)
}

// ParseAndRun parses YAML data and runs the workflow.
func (e *Engine) ParseAndRun(ctx context.Context, data []byte, transportID string) (*WorkflowResult, error) {
	spec, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return e.Run(ctx, spec, transportID)
}

func (e *Engine) finish(spec *WorkflowSpec, rs *runState, status string, startedAt time.Time) *WorkflowResult {
	writeResult(spec, rs, status, startedAt)
	wr := &WorkflowResult{
		Workflow: spec.Name,
		Status:   status,
		Duration: time.Since(startedAt).Round(time.Millisecond).String(),
		Steps:    buildStepResults(rs),
	}
	return wr
}

func (e *Engine) publishEvent(ctx context.Context, et event.Type, name string, payload map[string]any) {
	if e.eventBus == nil {
		return
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["workflow"] = name
	e.eventBus.Publish(ctx, event.Event{
		Type:      et,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}

func (e *Engine) publishStepEvent(ctx context.Context, name, stepID string, status StepStatus, result any) {
	if e.eventBus == nil {
		return
	}
	payload := map[string]any{
		"workflow": name,
		"step_id":  stepID,
		"status":   string(status),
	}
	if result != nil {
		payload["result"] = result
	}
	e.eventBus.Publish(ctx, event.Event{
		Type:      event.EventWorkflowStepChange,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}

func (rs *runState) specLookup(id string) *StepSpec {
	for i := range rs.spec.Steps {
		if rs.spec.Steps[i].ID == id {
			return &rs.spec.Steps[i]
		}
	}
	return nil
}

func allInstDone(results []InstanceResult) bool {
	if len(results) == 0 {
		return true // vacuous: empty foreach is done
	}
	for _, r := range results {
		if r.Status != StatusDone {
			return false
		}
	}
	return true
}

func allInstFailed(results []InstanceResult) bool {
	if len(results) == 0 {
		return false // empty is not a failure
	}
	for _, r := range results {
		if r.Status != StatusFailed {
			return false
		}
	}
	return true
}

func firstError(results []InstanceResult) string {
	for _, r := range results {
		if r.Error != "" {
			return r.Error
		}
	}
	return "unknown error"
}

func countDone(rs *runState) int {
	n := 0
	for _, st := range rs.statuses {
		if st == StatusDone {
			n++
		}
	}
	return n
}
