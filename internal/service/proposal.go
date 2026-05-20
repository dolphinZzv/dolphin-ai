package service

import (
	"fmt"
	"log"
	"time"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
	gormrepo "chick/internal/repository/gorm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProposalService struct {
	db           *gorm.DB
	proposalRepo repository.ProposalRepository
	taskRepo     repository.TaskRepository
	timelineRepo repository.TimelineRepository
	eventBus     *events.Bus
}

func NewProposalService(
	db *gorm.DB,
	proposalRepo repository.ProposalRepository,
	taskRepo repository.TaskRepository,
	timelineRepo repository.TimelineRepository,
	eventBus *events.Bus,
) *ProposalService {
	return &ProposalService{
		db:           db,
		proposalRepo: proposalRepo,
		taskRepo:     taskRepo,
		timelineRepo: timelineRepo,
		eventBus:     eventBus,
	}
}

var validProposalTransitions = map[models.ProposalState][]models.ProposalState{
	models.ProposalStateDraft:       {models.ProposalStateSubmitted, models.ProposalStateCancelled},
	models.ProposalStateSubmitted:   {models.ProposalStateUnderReview, models.ProposalStateCancelled},
	models.ProposalStateUnderReview: {models.ProposalStateApproved, models.ProposalStateRejected, models.ProposalStateDraft},
	models.ProposalStateApproved:    {models.ProposalStateInExecution},
	models.ProposalStateRejected:    {models.ProposalStateDraft},
	models.ProposalStateInExecution: {models.ProposalStateCompleted, models.ProposalStateCancelled},
	models.ProposalStateCompleted:   {},
	models.ProposalStateCancelled:   {},
}

func (s *ProposalService) ValidTransitions(state models.ProposalState) ([]models.ProposalState, error) {
	allowed, ok := validProposalTransitions[state]
	if !ok {
		return nil, fmt.Errorf("unknown proposal state: %s", state)
	}
	return allowed, nil
}

func (s *ProposalService) Create(projectID, authorID uint, title, description string, priority models.Priority, labelIDs []uint) (*models.Proposal, error) {
	var proposal *models.Proposal

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txProposalRepo := gormrepo.NewProposalRepo(tx)

		proposal = &models.Proposal{
			ProjectID:   projectID,
			Title:       title,
			Description: description,
			State:       models.ProposalStateDraft,
			Priority:    priority,
			AuthorID:    authorID,
		}
		if err := txProposalRepo.Create(proposal); err != nil {
			return fmt.Errorf("create proposal: %w", err)
		}

		for _, labelID := range labelIDs {
			if err := tx.Exec("INSERT INTO proposal_labels (proposal_id, label_id) VALUES (?, ?)", proposal.ID, labelID).Error; err != nil {
				return fmt.Errorf("add label %d: %w", labelID, err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Timeline event
	event := &models.TimelineEvent{
		ProposalID: &proposal.ID,
		ActorID:    authorID,
		EventType:  models.EventProposalCreated,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[proposal] failed to create timeline event: %v", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventProposalCreated,
			Payload: events.ProposalCreatedPayload{
				ProposalID: proposal.ID,
				ProjectID:  projectID,
				AuthorID:   authorID,
			},
		})
	}

	return s.proposalRepo.GetByID(proposal.ID)
}

func (s *ProposalService) GetByID(id uint) (*models.Proposal, error) {
	return s.proposalRepo.GetByID(id)
}

func (s *ProposalService) List(filter models.ProposalFilter) ([]models.Proposal, int64, error) {
	return s.proposalRepo.List(filter)
}

func (s *ProposalService) TransitionState(id uint, newState models.ProposalState, actorID uint, note *string) (*models.Proposal, error) {
	var oldState models.ProposalState
	var projectID uint

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current models.Proposal
		if err := tx.Model(&models.Proposal{}).Select("state,project_id").
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return fmt.Errorf("get proposal: %w", err)
		}
		oldState = current.State
		projectID = current.ProjectID

		allowed, ok := validProposalTransitions[current.State]
		if !ok {
			return fmt.Errorf("unknown current state: %s", current.State)
		}
		valid := false
		for _, s := range allowed {
			if s == newState {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("transition from %s to %s not allowed", current.State, newState)
		}

		txProposalRepo := gormrepo.NewProposalRepo(tx)
		if err := txProposalRepo.UpdateState(id, newState); err != nil {
			return err
		}

		now := time.Now()
		updates := map[string]interface{}{}
		if newState == models.ProposalStateSubmitted && current.SubmittedAt == nil {
			updates["submitted_at"] = now
		}
		if newState == models.ProposalStateApproved && current.ApprovedAt == nil {
			updates["approved_at"] = now
		}
		if newState == models.ProposalStateInExecution && current.StartedAt == nil {
			updates["started_at"] = now
		}
		if newState == models.ProposalStateCompleted && current.CompletedAt == nil {
			updates["completed_at"] = now
		}
		if newState == models.ProposalStateCancelled && current.CancelledAt == nil {
			updates["cancelled_at"] = now
		}
		if len(updates) > 0 {
			if err := tx.Model(&models.Proposal{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return fmt.Errorf("set timestamps: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	payload := models.JSONMap{"from": string(oldState), "to": string(newState)}
	if note != nil && *note != "" {
		payload["note"] = *note
	}
	event := &models.TimelineEvent{
		ProposalID: &id,
		ActorID:    actorID,
		EventType:  models.EventProposalStateChanged,
		Payload:    payload,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[proposal] failed to create timeline event: %v", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventProposalStateChanged,
			Payload: events.ProposalStateChangedPayload{
				ProposalID: id,
				ProjectID:  projectID,
				From:       string(oldState),
				To:         string(newState),
				ActorID:    actorID,
			},
		})
	}

	return s.proposalRepo.GetByID(id)
}

func (s *ProposalService) Review(id, reviewerID uint, approved bool, note *string) (*models.Proposal, error) {
	_, err := s.proposalRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get proposal: %w", err)
	}

	targetState := models.ProposalStateApproved
	if !approved {
		targetState = models.ProposalStateRejected
	}

	return s.transitionWithReview(id, targetState, reviewerID, note)
}

func (s *ProposalService) transitionWithReview(id uint, targetState models.ProposalState, reviewerID uint, note *string) (*models.Proposal, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Proposal{}).Where("id = ?", id).
			Updates(map[string]interface{}{
				"reviewer_id": reviewerID,
				"review_note": note,
				"reviewed_at": time.Now(),
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.TransitionState(id, targetState, reviewerID, note)
}

func (s *ProposalService) Update(id uint, changes map[string]interface{}) error {
	return s.proposalRepo.Update(id, changes)
}

func (s *ProposalService) Delete(id uint) error {
	return s.proposalRepo.Delete(id)
}

func (s *ProposalService) ListTimeline(proposalID uint) ([]models.TimelineEvent, error) {
	return s.timelineRepo.ListByProposal(proposalID)
}
