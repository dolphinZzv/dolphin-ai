package graph

import (
	"chick/internal/events"
	"chick/internal/notifications"
	"chick/internal/service"
	"time"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

type Resolver struct {
	ProjectSvc            *service.ProjectService
	AgentSvc              *service.AgentService
	IssueSvc              *service.IssueService
	CommentSvc            *service.CommentService
	ProposalSvc           *service.ProposalService
	TaskSvc               *service.TaskService
	WorkflowSvc           *service.WorkflowService
	FeedbackSvc           *service.FeedbackService
	NotifSvc              *notifications.Service
	EventBus              *events.Bus
	HumanReg bool
	LoginLimiter          *rateLimiter
}

func NewResolver(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	proposalSvc *service.ProposalService,
	taskSvc *service.TaskService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	notifSvc *notifications.Service,
	eventBus *events.Bus,
		allowHumanRegistration bool,
) *Resolver {
	return &Resolver{
		ProjectSvc:   projectSvc,
		AgentSvc:     agentSvc,
		IssueSvc:     issueSvc,
		CommentSvc:   commentSvc,
		ProposalSvc:  proposalSvc,
		TaskSvc:      taskSvc,
		WorkflowSvc:  workflowSvc,
		FeedbackSvc:  feedbackSvc,
		NotifSvc:     notifSvc,
		EventBus:     eventBus,
		HumanReg: allowHumanRegistration,
		LoginLimiter: newRateLimiter(10, 15*time.Minute),
	}
}

// Ensure events is imported
var _ = events.Bus{}
