package service

import (
	"fmt"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
)

type CommentService struct {
	commentRepo  repository.CommentRepository
	timelineRepo repository.TimelineRepository
	eventBus     *events.Bus
}

func NewCommentService(commentRepo repository.CommentRepository, timelineRepo repository.TimelineRepository, eventBus *events.Bus) *CommentService {
	return &CommentService{commentRepo: commentRepo, timelineRepo: timelineRepo, eventBus: eventBus}
}

func (s *CommentService) Create(issueID, authorID uint, body string, contentType models.CommentContentType, parentID *uint) (*models.Comment, error) {
	c := &models.Comment{
		IssueID:     issueID,
		AuthorID:    authorID,
		Body:        body,
		ContentType: contentType,
		ParentID:    parentID,
	}
	if err := s.commentRepo.Create(c); err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}

	// Add timeline event
	timeEvent := &models.TimelineEvent{
		IssueID:   issueID,
		ActorID:   authorID,
		EventType: models.EventCommentAdded,
		Payload:   nil,
	}
	_ = s.timelineRepo.Create(timeEvent)

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventCommentAdded,
			Payload: map[string]interface{}{
				"commentID": c.ID,
				"issueID":   issueID,
				"authorID":  authorID,
			},
		})
	}

	return c, nil
}

func (s *CommentService) GetByID(id uint) (*models.Comment, error) {
	return s.commentRepo.GetByID(id)
}

func (s *CommentService) ListByIssue(issueID uint) ([]models.Comment, error) {
	return s.commentRepo.ListByIssue(issueID)
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
