package service

import (
	"errors"
	"fmt"

	"chick/internal/models"
)

type WorkflowService struct {
	issueService *IssueService
}

func NewWorkflowService(issueService *IssueService) *WorkflowService {
	return &WorkflowService{issueService: issueService}
}

var validTransitions = map[models.IssueState][]models.IssueState{
	models.IssueStateOpen:              {models.IssueStateInProgress},
	models.IssueStateInProgress:        {models.IssueStateBlocked, models.IssueStateReview},
	models.IssueStateBlocked:           {models.IssueStateInProgress, models.IssueStateClosedNotPlanned},
	models.IssueStateReview:            {models.IssueStateInProgress, models.IssueStateClosedCompleted, models.IssueStateClosedNotPlanned},
	models.IssueStateClosedCompleted:   {},
	models.IssueStateClosedNotPlanned: {},
}

func (s *WorkflowService) Transition(issueID uint, toState models.IssueState, actorID uint) (*models.Issue, error) {
	issue, err := s.issueService.GetByID(issueID)
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	allowed, ok := validTransitions[issue.State]
	if !ok {
		return nil, fmt.Errorf("unknown current state: %s", issue.State)
	}

	valid := false
	for _, s := range allowed {
		if s == toState {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("transition from %s to %s not allowed", issue.State, toState)
	}

	return s.issueService.TransitionState(issueID, toState, actorID)
}

func (s *WorkflowService) ValidTransitions(state models.IssueState) ([]models.IssueState, error) {
	allowed, ok := validTransitions[state]
	if !ok {
		return nil, errors.New("unknown state")
	}
	return allowed, nil
}
