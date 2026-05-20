package service

import (
	"fmt"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
	gormrepo "chick/internal/repository/gorm"

	"gorm.io/gorm"
)

type CommentService struct {
	db           *gorm.DB
	commentRepo  repository.CommentRepository
	timelineRepo repository.TimelineRepository
	issueRepo    repository.IssueRepository
	proposalRepo repository.ProposalRepository
	taskRepo     repository.TaskRepository
	eventBus     *events.Bus
}

func NewCommentService(db *gorm.DB, commentRepo repository.CommentRepository, timelineRepo repository.TimelineRepository, issueRepo repository.IssueRepository, proposalRepo repository.ProposalRepository, taskRepo repository.TaskRepository, eventBus *events.Bus) *CommentService {
	return &CommentService{db: db, commentRepo: commentRepo, timelineRepo: timelineRepo, issueRepo: issueRepo, proposalRepo: proposalRepo, taskRepo: taskRepo, eventBus: eventBus}
}

func (s *CommentService) Create(issueID, authorID uint, body string, contentType models.CommentContentType, parentID *uint) (*models.Comment, error) {
	var c *models.Comment

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txCommentRepo := gormrepo.NewCommentRepo(tx)
		txTimelineRepo := gormrepo.NewTimelineRepo(tx)

		c = &models.Comment{
			IssueID:     &issueID,
			AuthorID:    authorID,
			Body:        body,
			ContentType: contentType,
			ParentID:    parentID,
		}
		if err := txCommentRepo.Create(c); err != nil {
			return fmt.Errorf("create comment: %w", err)
		}

		timeEvent := &models.TimelineEvent{
			IssueID:   &issueID,
			ActorID:   authorID,
			EventType: models.EventCommentAdded,
			Payload:   nil,
		}
		if err := txTimelineRepo.Create(timeEvent); err != nil {
			return fmt.Errorf("create timeline event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	projectID := uint(0)
	issue, err := s.issueRepo.GetByID(issueID)
	if err == nil {
		projectID = issue.ProjectID
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventCommentAdded,
			Payload: events.CommentAddedPayload{
				CommentID: c.ID,
				IssueID:   issueID,
				ProjectID: projectID,
				AuthorID:  authorID,
			},
		})
	}

	return s.commentRepo.GetByID(c.ID)
}

func (s *CommentService) GetByID(id uint) (*models.Comment, error) {
	return s.commentRepo.GetByID(id)
}

func (s *CommentService) ListByIssue(issueID uint) ([]models.Comment, error) {
	return s.commentRepo.ListByIssue(issueID)
}

func (s *CommentService) ListByProposal(proposalID uint) ([]models.Comment, error) {
	return s.commentRepo.ListByProposal(proposalID)
}

func (s *CommentService) ListByTask(taskID uint) ([]models.Comment, error) {
	return s.commentRepo.ListByTask(taskID)
}

func (s *CommentService) Update(id uint, body string) (*models.Comment, error) {
	if err := s.commentRepo.Update(id, body); err != nil {
		return nil, fmt.Errorf("update comment: %w", err)
	}
	return s.commentRepo.GetByID(id)
}

func (s *CommentService) Delete(id uint) error {
	return s.commentRepo.Delete(id)
}

func (s *CommentService) CreateForProposal(proposalID, authorID uint, body string, contentType models.CommentContentType, parentID *uint) (*models.Comment, error) {
	var c *models.Comment

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txCommentRepo := gormrepo.NewCommentRepo(tx)
		txTimelineRepo := gormrepo.NewTimelineRepo(tx)

		c = &models.Comment{
			ProposalID:  &proposalID,
			AuthorID:    authorID,
			Body:        body,
			ContentType: contentType,
			ParentID:    parentID,
		}
		if err := txCommentRepo.Create(c); err != nil {
			return fmt.Errorf("create comment: %w", err)
		}

		timeEvent := &models.TimelineEvent{
			ProposalID: &proposalID,
			ActorID:    authorID,
			EventType:  models.EventCommentAdded,
			Payload:    nil,
		}
		if err := txTimelineRepo.Create(timeEvent); err != nil {
			return fmt.Errorf("create timeline event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	projectID := uint(0)
	proposal, err := s.proposalRepo.GetByID(proposalID)
	if err == nil {
		projectID = proposal.ProjectID
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventCommentAdded,
			Payload: events.CommentAddedPayload{
				CommentID:  c.ID,
				ProposalID: &proposalID,
				ProjectID:  projectID,
				AuthorID:   authorID,
			},
		})
	}

	return s.commentRepo.GetByID(c.ID)
}

func (s *CommentService) CreateForTask(taskID, authorID uint, body string, contentType models.CommentContentType, parentID *uint) (*models.Comment, error) {
	var c *models.Comment

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txCommentRepo := gormrepo.NewCommentRepo(tx)
		txTimelineRepo := gormrepo.NewTimelineRepo(tx)

		c = &models.Comment{
			TaskID:      &taskID,
			AuthorID:    authorID,
			Body:        body,
			ContentType: contentType,
			ParentID:    parentID,
		}
		if err := txCommentRepo.Create(c); err != nil {
			return fmt.Errorf("create comment: %w", err)
		}

		timeEvent := &models.TimelineEvent{
			TaskID:    &taskID,
			ActorID:   authorID,
			EventType: models.EventCommentAdded,
			Payload:   nil,
		}
		if err := txTimelineRepo.Create(timeEvent); err != nil {
			return fmt.Errorf("create timeline event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	projectID := uint(0)
	task, err := s.taskRepo.GetByID(taskID)
	if err == nil {
		if task.Proposal.ProjectID != 0 {
			projectID = task.Proposal.ProjectID
		} else {
			proposal, err := s.proposalRepo.GetByID(task.ProposalID)
			if err == nil {
				projectID = proposal.ProjectID
			}
		}
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventCommentAdded,
			Payload: events.CommentAddedPayload{
				CommentID: c.ID,
				TaskID:    &taskID,
				ProjectID: projectID,
				AuthorID:  authorID,
			},
		})
	}

	return s.commentRepo.GetByID(c.ID)
}
