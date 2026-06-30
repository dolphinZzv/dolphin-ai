package agentmesh

import (
	"context"

	"dolphin/internal/workflow"
)

// WorkflowDelegator adapts *AgentMesh to the workflow.Delegator interface.
// workflow defines its own DelegatePayload/DelegateResult types to avoid
// importing agentmesh; this adapter translates between the two.
type WorkflowDelegator struct{ mesh *AgentMesh }

// NewWorkflowDelegator wraps an AgentMesh so it can be attached to a workflow
// Engine via engine.SetDelegator.
func NewWorkflowDelegator(mesh *AgentMesh) *WorkflowDelegator {
	return &WorkflowDelegator{mesh: mesh}
}

// Enabled reports whether the mesh is on.
func (w *WorkflowDelegator) Enabled() bool { return w.mesh != nil && w.mesh.Enabled() }

// Delegate translates the workflow payload to agentmesh's DelegatePayload,
// calls the mesh, and translates the result back.
func (w *WorkflowDelegator) Delegate(ctx context.Context, p workflow.DelegatePayload) (*workflow.DelegateResult, error) {
	if w.mesh == nil {
		return nil, nil
	}
	res, err := w.mesh.Delegate(ctx, DelegatePayload{
		Task:             p.Task,
		PreferredAgent:   p.PreferredAgent,
		ParentSessionID:  p.ParentSessionID,
		DelegationDepth:  p.DelegationDepth,
		Timeout:          p.Timeout,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	out := &workflow.DelegateResult{
		Status:  string(res.Status),
		Content: res.Content,
		Rounds:  res.Rounds,
	}
	if res.Error != nil {
		out.Error = res.Error.Error()
	}
	return out, nil
}

// Compile-time check that WorkflowDelegator satisfies workflow.Delegator.
var _ workflow.Delegator = (*WorkflowDelegator)(nil)
