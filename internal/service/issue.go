package service

import (
	"fmt"
	"log"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
	gormrepo "chick/internal/repository/gorm"

	"gorm.io/gorm"
)

type IssueService struct {
	db            *gorm.DB
	issueRepo     repository.IssueRepository
	assigneeRepo  repository.IssueAssigneeRepository
	timelineRepo  repository.TimelineRepository
	projectRepo   repository.ProjectRepository
	eventBus      *events.Bus
}

func NewIssueService(
	db *gorm.DB,
	issueRepo repository.IssueRepository,
	assigneeRepo repository.IssueAssigneeRepository,
	timelineRepo repository.TimelineRepository,
	projectRepo repository.ProjectRepository,
	eventBus *events.Bus,
) *IssueService {
	return &IssueService{
		db:           db,
		issueRepo:    issueRepo,
		assigneeRepo: assigneeRepo,
		timelineRepo: timelineRepo,
		projectRepo:  projectRepo,
		eventBus:     eventBus,
	}
}

func (s *IssueService) Create(projectID, creatorID uint, title, description string, priority models.Priority, assigneeIDs, labelIDs []uint, milestoneID *uint) (*models.Issue, error) {
	var issue *models.Issue

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txIssueRepo := gormrepo.NewIssueRepo(tx)
		txAssigneeRepo := gormrepo.NewIssueAssigneeRepo(tx)

		issue = &models.Issue{
			ProjectID:   projectID,
			Title:       title,
			Description: description,
			State:       models.IssueStateOpen,
			Priority:    priority,
			CreatorID:   creatorID,
			MilestoneID: milestoneID,
		}
		if err := txIssueRepo.Create(issue); err != nil {
			return fmt.Errorf("create issue: %w", err)
		}

		for _, agentID := range assigneeIDs {
			ia := &models.IssueAssignee{
				IssueID: issue.ID,
				AgentID: agentID,
				State:   models.AssigneeStatePending,
			}
			if err := txAssigneeRepo.Create(ia); err != nil {
				return fmt.Errorf("add assignee %d: %w", agentID, err)
			}
		}

		for _, labelID := range labelIDs {
			if err := txIssueRepo.AddLabel(issue.ID, labelID); err != nil {
				return fmt.Errorf("add label %d: %w", labelID, err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Timeline event (best-effort, outside transaction)
	event := &models.TimelineEvent{
		IssueID:   issue.ID,
		ActorID:   creatorID,
		EventType: models.EventIssueCreated,
		Payload:   nil,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[issue] failed to create timeline event: %v", err)
	}

	// Publish event (outside transaction)
	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueCreated,
			Payload: events.IssueCreatedPayload{
				IssueID:   issue.ID,
				ProjectID: projectID,
				CreatorID: creatorID,
				LabelIDs:  labelIDs,
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

	err = s.db.Transaction(func(tx *gorm.DB) error {
		txIssueRepo := gormrepo.NewIssueRepo(tx)
		return txIssueRepo.UpdateState(id, newState)
	})
	if err != nil {
		return nil, fmt.Errorf("transition state: %w", err)
	}

	// Timeline event (best-effort)
	event := &models.TimelineEvent{
		IssueID:   id,
		ActorID:   actorID,
		EventType: models.EventIssueStateChanged,
		Payload:   nil,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[issue] failed to create timeline event: %v", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueStateChanged,
			Payload: events.IssueStateChangedPayload{
				IssueID:   id,
				ProjectID: issue.ProjectID,
				From:      string(oldState),
				To:        string(newState),
				ActorID:   actorID,
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

	projectID := uint(0)
	issue, err := s.issueRepo.GetByID(issueID)
	if err == nil {
		projectID = issue.ProjectID
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueAssigneeChanged,
			Payload: events.IssueAssigneeChangedPayload{
				IssueID:   issueID,
				ProjectID: projectID,
				AgentID:   agentID,
				Action:    "assigned",
			},
		})
	}

	return ia, nil
}

func (s *IssueService) RemoveAssignee(issueID, agentID uint) error {
	if err := s.assigneeRepo.Remove(issueID, agentID); err != nil {
		return err
	}

	projectID := uint(0)
	issue, err := s.issueRepo.GetByID(issueID)
	if err == nil {
		projectID = issue.ProjectID
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventIssueAssigneeChanged,
			Payload: events.IssueAssigneeChangedPayload{
				IssueID:   issueID,
				ProjectID: projectID,
				AgentID:   agentID,
				Action:    "removed",
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

func (s *IssueService) Update(id uint, title, description string, priority models.Priority, dueDate *models.UnixNullTime, milestoneID *uint) (*models.Issue, error) {
	changes := map[string]interface{}{}
	if title != "" {
		changes["title"] = title
	}
	if description != "" {
		changes["description"] = description
	}
	if priority != "" {
		changes["priority"] = priority
	}
	if dueDate != nil && dueDate.Valid {
		changes["due_date"] = dueDate.Time
	}
	if milestoneID != nil {
		changes["milestone_id"] = *milestoneID
	}
	if len(changes) == 0 {
		return s.issueRepo.GetByID(id)
	}
	if err := s.issueRepo.Update(id, changes); err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}
	return s.issueRepo.GetByID(id)
}

func (s *IssueService) Delete(id uint) error {
	return s.issueRepo.Delete(id)
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
