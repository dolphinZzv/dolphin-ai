package service

import (
	"errors"

	"chick/internal/models"
)

type WorkflowService struct {
	issueService *IssueService
}

func NewWorkflowService(issueService *IssueService) *WorkflowService {
	return &WorkflowService{issueService: issueService}
}

var validTransitions = map[models.IssueState][]models.IssueState{
	models.IssueStateOpen:                {models.IssueStateInProgress, models.IssueStateBlocked, models.IssueStateLater, models.IssueStateClosedNotPlanned},
	models.IssueStateInProgress:          {models.IssueStateBlocked, models.IssueStateReview, models.IssueStateLater},
	models.IssueStateBlocked:             {models.IssueStateInProgress, models.IssueStateClosedNotPlanned, models.IssueStateLater},
	models.IssueStateReview:              {models.IssueStateInProgress, models.IssueStateClosedCompleted, models.IssueStateClosedNotPlanned, models.IssueStateClosedRejected, models.IssueStateLater, models.IssueStatePendingConfirmation},
	models.IssueStatePendingConfirmation: {models.IssueStateClosedCompleted, models.IssueStateInProgress, models.IssueStateClosedNotPlanned, models.IssueStateLater},
	models.IssueStateLater:               {models.IssueStateOpen, models.IssueStateClosedNotPlanned},
	models.IssueStateReopen:              {models.IssueStateInProgress, models.IssueStateBlocked, models.IssueStateLater},
	models.IssueStateClosedCompleted:     {models.IssueStateReopen},
	models.IssueStateClosedNotPlanned:    {models.IssueStateReopen},
	models.IssueStateClosedRejected:      {models.IssueStateReopen},
}

func (s *WorkflowService) Transition(issueID uint, toState models.IssueState, actorID uint) (*models.Issue, error) {
	// Delegates to IssueService.TransitionState which validates atomically in a transaction
	return s.issueService.TransitionState(issueID, toState, actorID)
}

func (s *WorkflowService) ValidTransitions(state models.IssueState) ([]models.IssueState, error) {
	allowed, ok := validTransitions[state]
	if !ok {
		return nil, errors.New("unknown state")
	}
	return allowed, nil
}
