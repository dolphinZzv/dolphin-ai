package matching

import (
	"log"
	"time"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
)

// Engine matches issues to agents based on label→capability mapping.
type Engine struct {
	agentRepo    repository.AgentRepository
	labelRepo    repository.LabelRepository
	assigneeRepo repository.IssueAssigneeRepository
	issueRepo    repository.IssueRepository
}

func NewEngine(
	agentRepo repository.AgentRepository,
	labelRepo repository.LabelRepository,
	assigneeRepo repository.IssueAssigneeRepository,
	issueRepo repository.IssueRepository,
) *Engine {
	return &Engine{
		agentRepo:    agentRepo,
		labelRepo:    labelRepo,
		assigneeRepo: assigneeRepo,
		issueRepo:    issueRepo,
	}
}

// Subscribe registers this engine as a handler for Issue.Created events.
func (e *Engine) Subscribe(bus *events.Bus) {
	bus.Subscribe(events.EventIssueCreated, e.handleIssueCreated)
	log.Println("[matching] subscribed to issue.created")
}

func (e *Engine) handleIssueCreated(evt events.Event) {
	payload, ok := evt.Payload.(events.IssueCreatedPayload)
	if !ok {
		return
	}

	if payload.IssueID == 0 || payload.ProjectID == 0 {
		return
	}

	// If labels are provided, try matching via label→capability→agent
	if len(payload.LabelIDs) > 0 {
		e.matchAndAssign(payload.IssueID, payload.ProjectID, payload.LabelIDs)
		return
	}

	// No labels — assign to any online project member
	e.assignAnyOnline(payload.IssueID, payload.ProjectID)
}

func (e *Engine) matchAndAssign(issueID, projectID uint, labelIDs []uint) {
	// Collect capabilities from labels
	capSet := make(map[models.CapabilityType]bool)
	for _, lid := range labelIDs {
		label, err := e.labelRepo.GetByID(lid)
		if err != nil || label == nil {
			continue
		}
		if label.Capability != "" {
			capSet[label.Capability] = true
		}
	}

	// Find agents matching any of the required capabilities
	matched := make(map[uint]bool)
	for capType := range capSet {
		agents, err := e.agentRepo.FindByCapability(capType, projectID)
		if err != nil {
			log.Printf("[matching] find by capability %s: %v", capType, err)
			continue
		}
		for _, a := range agents {
			matched[a.ID] = true
		}
	}

	if len(matched) == 0 {
		log.Printf("[matching] no agents matched for issue %d (project %d), falling back to any online agent", issueID, projectID)
		e.assignAnyOnline(issueID, projectID)
		return
	}

	// Assign to all matched agents
	for agentID := range matched {
		ia := &models.IssueAssignee{
			IssueID: issueID,
			AgentID: agentID,
			State:   models.AssigneeStatePending,
		}
		if err := e.assigneeRepo.Create(ia); err != nil {
			log.Printf("[matching] assign issue %d to agent %d: %v", issueID, agentID, err)
		} else {
			log.Printf("[matching] assigned issue %d to agent %d (capability match)", issueID, agentID)
		}
	}
}

func (e *Engine) assignAnyOnline(issueID, projectID uint) {
	agents, err := e.agentRepo.FindOnlineByProject(projectID)
	if err != nil {
		log.Printf("[matching] find online agents: %v", err)
		return
	}
	if len(agents) == 0 {
		log.Printf("[matching] no online agents for project %d", projectID)
		return
	}

	// Assign to first online agent
	agent := agents[0]
	ia := &models.IssueAssignee{
		IssueID: issueID,
		AgentID: agent.ID,
		State:   models.AssigneeStatePending,
	}
	if err := e.assigneeRepo.Create(ia); err != nil {
		log.Printf("[matching] assign issue %d to agent %d: %v", issueID, agent.ID, err)
	} else {
		log.Printf("[matching] auto-assigned issue %d to agent %d (any online)", issueID, agent.ID)
	}
}

// WatchOfflineTimeout scans for timed-out assignments and releases them.
// Meant to be run as a goroutine.
func (e *Engine) WatchOfflineTimeout(interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		e.releaseTimedOut(timeout)
	}
}

func (e *Engine) releaseTimedOut(timeout time.Duration) {
	// Find all pending/in-progress assignments where agent has no recent heartbeat
	// This is a simple query approach: check agents with last_seen_at older than timeout
	cutoff := time.Now().Add(-timeout)

	agents, err := e.agentRepo.FindTimedOut(cutoff)
	if err != nil {
		log.Printf("[matching] find timed-out agents: %v", err)
		return
	}

	for _, a := range agents {
		assignments, err := e.assigneeRepo.ListByAgent(a.ID)
		if err != nil {
			continue
		}
		for _, assignment := range assignments {
			if assignment.State == models.AssigneeStatePending || assignment.State == models.AssigneeStateInProgress {
				if err := e.assigneeRepo.UpdateState(assignment.IssueID, a.ID, models.AssigneeStateBlocked); err != nil {
					log.Printf("[matching] failed to release assignment issue %d from agent %d: %v", assignment.IssueID, a.ID, err)
				} else {
					log.Printf("[matching] released assignment issue %d from agent %d (timeout)", assignment.IssueID, a.ID)
				}
			}
		}
	}
}
