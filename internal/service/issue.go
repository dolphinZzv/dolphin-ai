package service

import (
	"fmt"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
)

type IssueService struct {
	issueRepo     repository.IssueRepository
	assigneeRepo  repository.IssueAssigneeRepository
	timelineRepo  repository.TimelineRepository
	projectRepo   repository.ProjectRepository
	eventBus      *events.Bus
}

func NewIssueService(
	issueRepo repository.IssueRepository,
	assigneeRepo repository.IssueAssigneeRepository,
	timelineRepo repository.TimelineRepository,
	projectRepo repository.ProjectRepository,
	eventBus *events.Bus,
) *IssueService {
	return &IssueService{
		issueRepo:    issueRepo,
		assigneeRepo: assigneeRepo,
		timelineRepo: timelineRepo,
		projectRepo:  projectRepo,
		eventBus:     eventBus,
	}
}

func (s *IssueService) Create(projectID, creatorID uint, title, description string, priority models.Priority, assigneeIDs []uint, labelIDs []uint) (*models.Issue, error) {
	number, err := s.issueRepo.NextNumber(projectID)
	if err != nil {
		return nil, fmt.Errorf("next number: %w", err)
	}

	issue := &models.Issue{
		Number:      number,
		ProjectID:   projectID,
		Title:       title,
		Description: description,
		State:       models.IssueStateOpen,
		Priority:    priority,
		CreatorID:   creatorID,
	}
	if err := s.issueRepo.Create(issue); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	// Add assignees
	for _, agentID := range assigneeIDs {
		ia := &models.IssueAssignee{
			IssueID: issue.ID,
			AgentID: agentID,
			State:   models.AssigneeStatePending,
		}
		if err := s.assigneeRepo.Create(ia); err != nil {
			return nil, fmt.Errorf("add assignee %d: %w", agentID, err)
		}
	}

	// Add labels
	for _, labelID := range labelIDs {
		if err := s.issueRepo.AddLabel(issue.ID, labelID); err != nil {
			return nil, fmt.Errorf("add label %d: %w", labelID, err)
		}
	}

	// Add timeline event
	event := &models.TimelineEvent{
		IssueID:   issue.ID,
		ActorID:   creatorID,
		EventType: models.EventIssueCreated,
		Payload:   nil,
	}
	_ = s.timelineRepo.Create(event)

	// Publish event
	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueCreated,
			Payload: map[string]interface{}{
				"issueID":   issue.ID,
				"projectID": projectID,
				"creatorID": creatorID,
				"labelIDs":  labelIDs,
			},
		})
	}

	return s.issueRepo.GetByID(issue.ID)
}

func (s *IssueService) GetByID(id uint) (*models.Issue, error) {
	return s.issueRepo.GetByID(id)
}

func (s *IssueService) GetByNumber(projectID uint, number uint) (*models.Issue, error) {
	return s.issueRepo.GetByNumber(projectID, number)
}

func (s *IssueService) List(filter models.IssueFilter) ([]models.Issue, int64, error) {
	return s.issueRepo.List(filter)
}

func (s *IssueService) TransitionState(id uint, newState models.IssueState, actorID uint) (*models.Issue, error) {
	issue, err := s.issueRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get issue for transition: %w", err)
	}
	oldState := issue.State

	if err := s.issueRepo.UpdateState(id, newState); err != nil {
		return nil, fmt.Errorf("transition state: %w", err)
	}
	event := &models.TimelineEvent{
		IssueID:   id,
		ActorID:   actorID,
		EventType: models.EventIssueStateChanged,
		Payload:   nil,
	}
	_ = s.timelineRepo.Create(event)

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueStateChanged,
			Payload: map[string]interface{}{
				"issueID":  id,
				"from":     string(oldState),
				"to":       string(newState),
				"actorID":  actorID,
			},
		})
	}

	return s.issueRepo.GetByID(id)
}

func (s *IssueService) AddAssignee(issueID, agentID uint) (*models.IssueAssignee, error) {
	ia := &models.IssueAssignee{
		IssueID: issueID,
		AgentID: agentID,
		State:   models.AssigneeStatePending,
	}
	if err := s.assigneeRepo.Create(ia); err != nil {
		return nil, fmt.Errorf("add assignee: %w", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueAssigneeChanged,
			Payload: map[string]interface{}{
				"issueID": issueID,
				"agentID": agentID,
				"action":  "assigned",
			},
		})
	}

	return ia, nil
}

func (s *IssueService) RemoveAssignee(issueID, agentID uint) error {
	if err := s.assigneeRepo.Remove(issueID, agentID); err != nil {
		return err
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueAssigneeChanged,
			Payload: map[string]interface{}{
				"issueID": issueID,
				"agentID": agentID,
				"action":  "removed",
			},
		})
	}

	return nil
}

func (s *IssueService) UpdateAssigneeState(issueID, agentID uint, state models.AssigneeState) (*models.IssueAssignee, error) {
	if err := s.assigneeRepo.UpdateState(issueID, agentID, state); err != nil {
		return nil, fmt.Errorf("update assignee state: %w", err)
	}
	list, err := s.assigneeRepo.ListByIssue(issueID)
	if err != nil {
		return nil, err
	}
	for _, ia := range list {
		if ia.AgentID == agentID {
			return &ia, nil
		}
	}
	return nil, fmt.Errorf("assignee not found")
}

func (s *IssueService) ListTimeline(issueID uint) ([]models.TimelineEvent, error) {
	return s.timelineRepo.ListByIssue(issueID)
}

func (s *IssueService) AddLabel(issueID, labelID uint) error {
	return s.issueRepo.AddLabel(issueID, labelID)
}

func (s *IssueService) RemoveLabel(issueID, labelID uint) error {
	return s.issueRepo.RemoveLabel(issueID, labelID)
}
