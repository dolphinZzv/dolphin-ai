package service

import (
	"fmt"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
)

type FeedbackService struct {
	feedbackRepo repository.FeedbackRepository
	eventBus     *events.Bus
}

func NewFeedbackService(feedbackRepo repository.FeedbackRepository, eventBus *events.Bus) *FeedbackService {
	return &FeedbackService{feedbackRepo: feedbackRepo, eventBus: eventBus}
}

func (s *FeedbackService) Create(targetType models.FeedbackTargetType, targetID, authorID uint, rating int, body string) (*models.Feedback, error) {
	f := &models.Feedback{
		TargetType: targetType,
		TargetID:   targetID,
		AuthorID:   authorID,
		Rating:     rating,
		Body:       body,
	}
	if err := s.feedbackRepo.Create(f); err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventFeedbackCreated,
			Payload: map[string]interface{}{
				"feedbackID": f.ID,
				"targetType": string(targetType),
				"targetID":   targetID,
				"authorID":   authorID,
			},
		})
	}

	return f, nil
}

func (s *FeedbackService) ListByTarget(targetType models.FeedbackTargetType, targetID uint) ([]models.Feedback, error) {
	return s.feedbackRepo.ListByTarget(targetType, targetID)
}
